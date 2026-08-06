package vow

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var migrationFilenamePattern = regexp.MustCompile(`^\d{6}_[a-z0-9_]+\.sql$`)

type Migrator struct {
	db            *pgxpool.Pool
	logger        *slog.Logger
	migrationsDir string
	tableName     string
	lockName      string
}

// Option allows for functional configuration of the Migrator
type Option func(*Migrator)

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

func (m *Migrator) Up(ctx context.Context) error {
	logger := m.logger.With("component", "migrations", "dir", m.migrationsDir)
	start := time.Now()

	conn, err := m.db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	// Session-level advisory lock
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtextextended($1, 0))", m.lockName); err != nil {
		return fmt.Errorf("acquire migration lock (%s): %w", m.lockName, err)
	}

	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanupCtx, "SELECT pg_advisory_unlock(hashtextextended($1, 0))", m.lockName); err != nil {
			logger.Warn("failed to release advisory lock", "lock_name", m.lockName, "err", err)
			return
		}
	}()

	// Ensure migrations tracking table exists
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`, m.tableName)
	if _, err := conn.Exec(ctx, query); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	migrationFiles, err := listSQLMigrations(m.migrationsDir)
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}

	applied, err := m.loadAppliedMigrations(ctx, conn)
	if err != nil {
		return err
	}

	if err = validateAppliedVersionsExistsOnDisk(applied, migrationFiles); err != nil {
		return err
	}

	// Execute only pending migrations, each in its own transaction
	for _, filename := range migrationFiles {
		if _, ok := applied[filename]; ok {
			continue
		}

		if err := m.applyMigration(ctx, conn, filename); err != nil {
			return err
		}
	}

	logger.Info("migration run complete", "duration_ms", time.Since(start).Milliseconds())
	return nil
}

func (m *Migrator) applyMigration(ctx context.Context, conn *pgxpool.Conn, filename string) error {
	fullPath := filepath.Join(m.migrationsDir, filename)
	sqlBytes, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("read migration file %s: %w", filename, err)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for %s: %s", filename, err)
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("exec migration %s: %w", filename, err)
	}

	query := fmt.Sprintf("INSERT INTO %s(version) VALUES($1)", m.tableName)
	if _, err = tx.Exec(ctx, query, filename); err != nil {
		return fmt.Errorf("track migration %s: %w", filename, err)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", filename, err)
	}

	m.logger.Info("applied migration", "version", filename)
	return nil
}

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
