package vow

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

func listSQLMigrations(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]string) // normalized -> original
	var out []string

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".sql") {
			continue
		}

		if !migrationFilenamePattern.MatchString(name) {
			return nil, fmt.Errorf(
				"invalid migration filename %q in %s: expected format %q (example: 001_create_tasks.sql)",
				name, dir, `NNN_snake_case.sql`,
			)
		}

		normalized := strings.ToLower(name)
		if prev, ok := seen[normalized]; ok {
			return nil, fmt.Errorf("duplicate migration filename detected (case-insensitive): %q and %q in %s", prev, name, dir)
		}

		seen[normalized] = name
		out = append(out, name)
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
