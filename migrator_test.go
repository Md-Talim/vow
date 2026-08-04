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
	migrationFile := filepath.Join(tmpDir, "000001_test.sql")
	if err := os.WriteFile(migrationFile, []byte("CREATE TABLE users (id SERIAL PRIMARY KEY);"), 0644); err != nil {
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
	m := New(pool, tmpDir, WithTableName("test_schema_migrations"))
	if err := m.Run(ctx); err != nil {
		t.Fatalf("migration failed: %v", err)
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
}
