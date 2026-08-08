package vow

import (
	"os"
	"testing"
)

func TestListSQLMigrations(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "test-migrations")
	defer os.RemoveAll(tmpDir)

	files := []string{"000001_init.up.sql", "000002_user.up.sql"}
	for _, f := range files {
		os.WriteFile(tmpDir+"/"+f, []byte(""), 0644)
	}

	migrations, err := listSQLMigrations(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(migrations) != 2 {
		t.Errorf("expected 2 migrations, got %d", len(migrations))
	}
}

func TestListSQLMigrations_InvalidFilename(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "test_invalid")
	defer os.RemoveAll(tmpDir)

	os.WriteFile(tmpDir+"/invalid.sql", []byte(""), 0644)
	_, err := listSQLMigrations(tmpDir)
	if err == nil {
		t.Error("expected error for invalid filename, got nil")
	}
}
