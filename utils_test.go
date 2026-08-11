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

	migrations, err := listSQLMigrations(os.DirFS(tmpDir))
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
	_, err := listSQLMigrations(os.DirFS(tmpDir))
	if err == nil {
		t.Error("expected error for invalid filename, got nil")
	}
}

func TestValidateMigrationFiles(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "test-migrations")
	defer os.RemoveAll(tmpDir)

	files := []string{"000001_init.up.sql", "000001_init.down.sql"}
	for _, f := range files {
		os.WriteFile(tmpDir+"/"+f, []byte(""), 0644)
	}
}

func TestValidateMigrationFiles_MissingPair(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "test-missing-pair")
	defer os.RemoveAll(tmpDir)

	// Only .up.sql, no .down.sql
	os.WriteFile(tmpDir+"/000001_init.up.sql", []byte(""), 0644)

	err := validateMigrationFiles(os.DirFS(tmpDir))
	if err == nil {
		t.Fatal("expected error due to missing pair, got nil")
	}
}

func TestValidateMigrationFiles_InvalidFilename(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "test-invalid-filename")
	defer os.RemoveAll(tmpDir)

	// Filename not matching expected pattern
	os.WriteFile(tmpDir+"/bad_name.sql", []byte(""), 0644)

	err := validateMigrationFiles(os.DirFS(tmpDir))
	if err == nil {
		t.Fatal("expected error due to invalid filename, got nil")
	}
}

func TestValidateMigrationFiles_Subdirectory(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "test-subdir")
	defer os.RemoveAll(tmpDir)

	os.Mkdir(tmpDir+"/subdir", 0755)

	err := validateMigrationFiles(os.DirFS(tmpDir))
	if err == nil {
		t.Fatal("expected error due to subdirectory, got nil")
	}
}
