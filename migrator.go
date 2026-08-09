package vow

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var migrationFilenamePattern = regexp.MustCompile(`^\d{6}_[a-z0-9_]+\.(up|down)\.sql$`)

type Migrator struct {
	db            *pgxpool.Pool
	logger        *slog.Logger
	migrationsDir string
	tableName     string
	lockName      string
}

// Result reports the outcome of an Up or Down run.
type Result struct {
	// Versions lists the migrations that changed state during this run,
	// in the order they were applied (Up) or rolled back (Down).
	// Empty if nothing needed to happen.
	Versions []string

	// Skipped is the number of already-applied migrations left untouched.
	// Always 0 for Down.
	Skipped int

	// Duration is the total wall-clock time for the run.
	Duration time.Duration
}

// Option allows for functional configuration of the Migrator
type Option func(*Migrator)

// New creates a new Migrator instance with the provided
// database connection pool and migrations directory.
func New(db *pgxpool.Pool, migrationsDir string, opts ...Option) *Migrator {
	m := &Migrator{
		db:            db,
		migrationsDir: migrationsDir,
		logger:        slog.Default(),
		tableName:     "schema_migrations",
		lockName:      "vow:migrations",
	}

	for _, opt := range opts {
		opt(m)
	}

	m.logger = m.logger.With("component", "migrations", "dir", m.migrationsDir)
	return m
}

func WithLogger(l *slog.Logger) Option {
	return func(m *Migrator) { m.logger = l }
}

func WithTableName(name string) Option {
	return func(m *Migrator) { m.tableName = name }
}

func WithLockName(name string) Option {
	return func(m *Migrator) { m.lockName = name }
}

// Up applies all pending migrations in the specified directory to the database.
func (m *Migrator) Up(ctx context.Context) (Result, error) {
	start := time.Now()
	result := Result{}

	conn, err := m.db.Acquire(ctx)
	if err != nil {
		return result, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	// Session-level advisory lock
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtextextended($1, 0))", m.lockName); err != nil {
		return result, fmt.Errorf("acquire migration lock (%s): %w", m.lockName, err)
	}

	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanupCtx, "SELECT pg_advisory_unlock(hashtextextended($1, 0))", m.lockName); err != nil {
			m.logger.Warn("failed to release advisory lock", "lock_name", m.lockName, "err", err)
			return
		}
	}()

	// Ensure migrations tracking table exists
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`, m.tableName)
	if _, err := conn.Exec(ctx, query); err != nil {
		return result, fmt.Errorf("create migration table: %w", err)
	}

	migrationFiles, err := listSQLMigrations(m.migrationsDir)
	if err != nil {
		return result, fmt.Errorf("list migrations: %w", err)
	}

	applied, err := m.loadAppliedMigrations(ctx, conn)
	if err != nil {
		return result, err
	}

	if err = validateAppliedVersionsExistsOnDisk(applied, migrationFiles); err != nil {
		return result, err
	}

	// Execute only pending migrations, each in its own transaction
	for _, filename := range migrationFiles {
		if _, ok := applied[filename]; ok {
			result.Skipped++
			continue
		}

		if err := m.applyMigration(ctx, conn, filename); err != nil {
			return result, err
		}
		result.Versions = append(result.Versions, strings.TrimSuffix(filename, ".up.sql"))
	}

	result.Duration = time.Since(start)
	m.logger.Info("migration run complete", "duration_ms", result.Duration.Milliseconds())
	return result, nil
}

// Down rolls back the last `steps` applied migrations in reverse order.
func (m *Migrator) Down(ctx context.Context, steps int) (Result, error) {
	start := time.Now()
	result := Result{}

	if steps <= 0 {
		return result, fmt.Errorf("steps must be greater than 0")
	}

	conn, err := m.db.Acquire(ctx)
	if err != nil {
		return result, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtextextended($1, 0))", m.lockName); err != nil {
		return result, fmt.Errorf("acquire migration lock (%s): %w", m.lockName, err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanupCtx, "SELECT pg_advisory_unlock(hashtextextended($1, 0))", m.lockName); err != nil {
			m.logger.Warn("failed to release advisory lock", "lock_name", m.lockName, "err", err)
		}
	}()

	applied, err := m.loadMigrationsToRollback(ctx, conn, steps)
	if err != nil {
		return result, fmt.Errorf("list migrations to rollback: %w", err)
	}

	for _, version := range applied {
		if err = m.rollbackMigration(ctx, conn, version); err != nil {
			return result, err
		}
		result.Versions = append(result.Versions, version)
	}

	result.Duration = time.Since(start)
	m.logger.Info("rollback run complete", "duration_ms", result.Duration.Milliseconds())
	return result, nil
}

// applyMigration executes a single migration file within a transaction
// and records its version in the migrations table.
func (m *Migrator) applyMigration(ctx context.Context, conn *pgxpool.Conn, filename string) error {
	fullPath := filepath.Join(m.migrationsDir, filename)
	sqlBytes, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("read migration file %s: %w", filename, err)
	}

	versionID := strings.TrimSuffix(filename, ".up.sql")

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for %s: %s", filename, err)
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("exec migration %s: %w", filename, err)
	}

	query := fmt.Sprintf("INSERT INTO %s(version) VALUES($1)", m.tableName)
	if _, err = tx.Exec(ctx, query, versionID); err != nil {
		return fmt.Errorf("track migration %s: %w", filename, err)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", filename, err)
	}

	m.logger.Info("applied migration", "version", versionID)
	return nil
}

func (m *Migrator) rollbackMigration(ctx context.Context, conn *pgxpool.Conn, version string) error {
	filename := fmt.Sprintf("%s.down.sql", version)
	fullPath := filepath.Join(m.migrationsDir, filename)
	sqlBytes, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("read file for rollback %s: %w", filename, err)
	}

	m.logger.Debug("rolling back migration", "version", version)

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for %s: %w", filename, err)
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("exec rollback %s: %w", filename, err)
	}

	query := fmt.Sprintf("DELETE FROM %s WHERE version = $1", m.tableName)
	if _, err = tx.Exec(ctx, query, version); err != nil {
		return fmt.Errorf("delete migration record %s: %w", version, err)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit rollback %s: %w", filename, err)
	}

	m.logger.Info("rollback migration", "version", version)
	return nil
}

// loadAppliedMigrations retrieves the set of applied migration versions from the database.
func (m *Migrator) loadAppliedMigrations(ctx context.Context, conn *pgxpool.Conn) (map[string]struct{}, error) {
	query := fmt.Sprintf(`SELECT version FROM %s ORDER BY version`, m.tableName)
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("read applied versions: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]struct{})

	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan applied version: %w", err)
		}
		applied[version] = struct{}{}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied versions: %w", err)
	}
	return applied, nil
}

func (m *Migrator) loadMigrationsToRollback(ctx context.Context, conn *pgxpool.Conn, steps int) ([]string, error) {
	query := fmt.Sprintf(`SELECT version FROM %s ORDER BY version DESC LIMIT $1`, m.tableName)
	rows, err := conn.Query(ctx, query, steps)
	if err != nil {
		return nil, fmt.Errorf("read applied versions for rollback: %w", err)
	}
	defer rows.Close()

	applied := []string{}

	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan applied version for rollback: %w", err)
		}
		applied = append(applied, version)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied versions for rollback: %w", err)
	}

	return applied, nil
}
