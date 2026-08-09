package vow

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrator(t *testing.T) {
	// 1. Skip if no database is available.
	// This allows the test to be safe for CI environments that don't have Postgres.
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration tests")
	}

	// 2. Setup a temporary directory for migration files
	tmpDir, err := os.MkdirTemp("", "migrations")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a dummy migration file
	migrationFile := filepath.Join(tmpDir, "000001_test.up.sql")
	if err := os.WriteFile(migrationFile, []byte("CREATE TABLE users (id SERIAL PRIMARY KEY);"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a dummy migration down file
	downFile := filepath.Join(tmpDir, "000001_test.down.sql")
	if err := os.WriteFile(downFile, []byte("DROP TABLE users;"), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Connect to DB
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer pool.Close()

	// 4. Run the actual migrator
	m, err := New(pool, tmpDir, WithTableName("test_schema_migrations"))
	result, err := m.Up(ctx)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if len(result.Versions) != 1 || result.Versions[0] != "000001_test" {
		t.Fatalf("expected 1 migration to be applied, got: %v", result.Versions)
	}

	// 5. Verification (Sanity check)
	// Check if table was created
	var exists bool
	err = pool.QueryRow(ctx, "SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'users')").Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("expected users table to exists, but it was not found")
	}

	// 6. Run migration rollback with Down()
	result, err = m.Down(ctx, 1)
	if err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	if len(result.Versions) != 1 || result.Versions[0] != "000001_test" {
		t.Errorf("expected 1 migration to be rolled back, got: %v", result.Versions)
	}

	// Check if the table was deleted
	err = pool.QueryRow(ctx, "SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'users')").Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("expected users table to not exists, but it exists")
	}
}
