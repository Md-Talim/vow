package vow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func listSQLMigrations(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]string)
	var out []string

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".sql") {
			continue
		}

		// Strictly enforce the format NNNNNN_name.up.sql or NNNNNN_name.down.sql
		if !migrationFilenamePattern.MatchString(name) {
			return nil, fmt.Errorf(
				"invalid migration filename %q in %s: expected format NNNNNN_name.up.sql or NNNNNN_name.down.sql",
				name, dir,
			)
		}

		normalized := strings.ToLower(name)
		if prev, ok := seen[normalized]; ok {
			return nil, fmt.Errorf("duplicate migration filename detected (case-insensitive): %q and %q in %s", prev, name, dir)
		}

		seen[normalized] = name
		// FILTER: Only include the "up" migrations in the list of files to apply.
		if strings.HasSuffix(normalized, ".up.sql") {
			out = append(out, name)
		}
	}

	sort.Strings(out)
	return out, nil
}

func validateAppliedVersionsExistsOnDisk(appliedMigrations map[string]struct{}, migrationFiles []string) error {
	onDisk := make(map[string]struct{}, len(migrationFiles))
	for _, f := range migrationFiles {
		onDisk[f] = struct{}{}
	}

	var missing []string
	for migration := range appliedMigrations {
		if _, ok := onDisk[migration]; !ok {
			missing = append(missing, migration)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf(
			`migration history mismatch: applied version(s) missing on disk: %s.
			Migration filenames are immutable version IDs; do not rename or delete applied migration files.`,
			strings.Join(missing, ", "),
		)
	}

	return nil
}

func validateMigrationFiles(migrationsDir string) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations directory: %w", err)
	}

	seen := make(map[string]bool) // filename -> pair confirmed

	for _, e := range entries {
		name := e.Name()

		if e.IsDir() {
			return fmt.Errorf("unexpected subdirectory %q in migrations directory: only .up.sql/.down.sql files are allowed", name)
		}

		if !migrationFilenamePattern.MatchString(name) {
			return fmt.Errorf("invalid migration filename %q: expected format NNNNNN_name.up.sql or NNNNNN_name.down.sql", name)
		}

		if seen[name] {
			continue
		}

		var pairName string
		switch {
		case strings.HasSuffix(name, ".up.sql"):
			pairName = strings.TrimSuffix(name, ".up.sql") + ".down.sql"
		case strings.HasSuffix(name, ".down.sql"):
			pairName = strings.TrimSuffix(name, ".down.sql") + ".up.sql"
		}

		if _, err := os.Stat(filepath.Join(migrationsDir, pairName)); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("missing migration pair for %q: expected %q to also exists", name, pairName)
			}
			return fmt.Errorf("check pair for %q: %w", name, err)
		}

		seen[name] = true
		seen[pairName] = true
	}

	return nil
}
