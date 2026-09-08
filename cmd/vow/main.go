// Command vow is a minimal CLI for the Vow PostgreSQL migration runner.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const usageText = `vow — PostgreSQL migration runner

Usage:
  vow <command> [flags]

Commands:
  up    Apply all pending migrations
  down  Roll back the N most recently applied migrations (default 1)

Run "vow <command> -h" for command-specific flags.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "vow: interrupted")
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, "vow:", err)
		os.Exit(1)
	}
}

// run parses the top-level command and dispatches to its handler.
func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usageText)
		return errors.New("missing command")
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "up":
		return cmdUp(ctx, rest)
	case "down":
		return cmdDown(ctx, rest)
	case "help", "-h", "--help":
		fmt.Fprint(os.Stdout, usageText)
		return nil
	default:
		fmt.Fprint(os.Stderr, usageText)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// config holds the flags shared by every subcommand.
type config struct {
	databaseURL   string
	migrationsDir string
	tableName     string
	lockName      string
}

// newFlagSet returns a FlagSet with the flags shared by all subcommands,
// bound to cfg. The database URL is resolved separately (see
// newMigrator) so a password in $DATABASE_URL never shows up in help text.
func newFlagSet(name string, cfg *config) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.databaseURL, "database-url", "", "PostgreSQL connection URL (overrides $DATABASE_URL)")
	fs.StringVar(&cfg.migrationsDir, "migrations-dir", "./migrations", "path to the migrations directory")
	fs.StringVar(&cfg.tableName, "table-name", "schema_migrations", "name of the migration tracking table")
	fs.StringVar(&cfg.lockName, "lock-name", "vow:migrations", "name of the advisory lock")
	return fs
}
