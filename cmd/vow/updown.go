package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/md-talim/vow"
)

// cmdUp applies all pending migrations.
func cmdUp(ctx context.Context, args []string) error {
	cfg := &config{}
	fs := newFlagSet("up", cfg)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("up takes no arguments, got %q", fs.Args())
	}

	m, pool, err := newMigrator(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	result, err := m.Up(ctx)
	if err != nil {
		return fmt.Errorf("migration run failed after applying %v: %w", result.Versions, err)
	}
	fmt.Printf("applied %d, skipped %d, took %s\n", len(result.Versions), result.Skipped, result.Duration.Round(time.Millisecond))
	return nil
}

// cmdDown rolls back the N most recently applied migrations.
func cmdDown(ctx context.Context, args []string) error {
	cfg := &config{}
	fs := newFlagSet("down", cfg)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	steps, err := parseSteps(fs.Args())
	if err != nil {
		return err
	}

	m, pool, err := newMigrator(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	result, err := m.Down(ctx, steps)
	if err != nil {
		return fmt.Errorf("rollback failed after rolling back %v: %w", result.Versions, err)
	}
	fmt.Printf("rolled back %d (%v), took %s\n", len(result.Versions), result.Versions, result.Duration.Round(time.Millisecond))
	return nil
}

// parseSteps turns the positional arguments of "down" into a step count.
// It defaults to 1 and rejects anything that is not a positive integer.
func parseSteps(args []string) (int, error) {
	if len(args) > 1 {
		return 0, fmt.Errorf("down takes at most one argument (steps), got %q", args)
	}
	if len(args) == 0 {
		return 1, nil
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid steps %q: must be a positive integer", args[0])
	}
	return n, nil
}

// newMigrator resolves the database URL and builds a Migrator from cfg,
// verifying the connection and the migrations directory up front.
func newMigrator(ctx context.Context, cfg *config) (*vow.Migrator, *pgxpool.Pool, error) {
	url := cfg.databaseURL
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		return nil, nil, errors.New("no database URL: set --database-url or $DATABASE_URL")
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, nil, fmt.Errorf("parse database URL: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("connect to database: %w", err)
	}

	m, err := vow.New(pool, os.DirFS(cfg.migrationsDir),
		vow.WithTableName(cfg.tableName),
		vow.WithLockName(cfg.lockName),
	)
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("invalid migrations directory: %w", err)
	}
	return m, pool, nil
}
