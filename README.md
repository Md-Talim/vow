# Vow

[![Go version](https://img.shields.io/github/go-mod/go-version/md-talim/vow)](https://go.dev/doc/install) 
[![License: MIT](https://img.shields.io/github/license/md-talim/vow)](LICENSE)
[![Tests](https://github.com/md-talim/vow/actions/workflows/ci.yml/badge.svg)](https://github.com/md-talim/vow/actions/workflows/ci.yml) 

Vow is a lightweight, opinionated migration runner for Go projects using `pgx` and PostgreSQL.

It is designed to be embedded directly into your application, ensuring your database schema is up-to-date before your service starts.

[Quick Start](#quick-start) • [Command Line](#command-line) • [Migration File Format](#migration-file-format) • [Design Notes](#design-notes)

## Motivation

I wanted to understand how database migration tools work internally. Having already used tools like Goose and Flyway, I didn't build Vow because I was missing a tool, I wanted to see what happens underneath the abstraction. I built a migration runner from scratch while working on [dhara](https://github.com/Md-Talim/dhara).

Once it was working, I extracted it from dhara into Vow to reuse it across my other Go projects. Dhara now uses Vow for its own migrations. This turned a learning exercise into a small, reusable library I can use whenever I need PostgreSQL migrations in a Go project.

### Key Features

- **Zero Dependencies:** Only relies on `pgx/v5`.
- **Safety First:** Uses PostgreSQL advisory locks to ensure multiple service instances don't run migrations simultaneously.
- **Simple:** No configuration files or complex CLI tools. Just code.
- **Immutable:** Enforces strict validation that applied migrations cannot be renamed, deleted, or edited (checksums are verified on every run).
- **Reversible:** Every migration is paired with a `.down.sql` counterpart, validated up front.

## Quick Start

1. **Install:**

    ```bash
    go get github.com/md-talim/vow
    ```

2. **Usage:** Create a directory (e.g., `./migrations`) and add paired `.up.sql` / `.down.sql` files.

    ```go
    import (
        "context"
        "log"
        "os"

        "github.com/md-talim/vow"
    )

    func main() {
        // ... initialize your pgxpool.Pool ...

        migrator, err := vow.New(dbPool, os.DirFS("./migrations"))
        if err != nil {
            log.Fatalf("invalid migrations directory: %v", err)
        }

        if _, err := migrator.Up(context.Background()); err != nil {
            log.Fatalf("failed to migrate: %v", err)
        }
    }
    ```

## Rolling Back

`Down` rolls back the N most recently applied migrations, in reverse order. Each rollback runs its paired `.down.sql` file and removes the tracking row in a single transaction.

```go
// Roll back the single most recent migration
result, err := migrator.Down(context.Background(), 1)
if err != nil {
    log.Fatalf("rollback failed: %v", err)
}
log.Printf("rolled back: %v", result.Versions)
```

If `steps` exceeds the number of applied migrations, Vow rolls back everything and stops, it will not error.

`Down` is intended for local development and test teardown, not automated production recovery. If `Up` fails mid-deploy, Vow does not attempt to roll back automatically, it fails loudly and leaves the decision (fix-forward vs. manual rollback) to a human. See [Design Notes](#design-notes) below.

## Result

Both `Up` and `Down` return a `Result` alongside the error, reporting what actually happened:

```go
type Result struct {
    Versions []string      // migrations applied (Up) or rolled back (Down), in order
    Skipped  int           // already-applied migrations left untouched (Up only; always 0 for Down)
    Duration time.Duration // total wall-clock time for the run
}
```

On a partial failure (e.g. migration 3 of 5 fails), `Result` still reflects whatever succeeded before the error; check both return values, not just the error.

```go
result, err := migrator.Up(ctx)
if err != nil {
    log.Printf("migration run failed after applying %v: %v", result.Versions, err)
    return err
}
log.Printf("applied %d, skipped %d, took %s", len(result.Versions), result.Skipped, result.Duration)
```

## Configuration

You can customize the migration behavior using functional options:

```go
migrator := vow.New(dbPool, "./migrations",
    vow.WithTableName("app_schema_migrations"),
    vow.WithLogger(myCustomLogger),
)
```

## Embedding Migrations

Vow accepts any `fs.FS`, so migration files can be compiled directly into your binary with `//go:embed`, which is useful when shipping migrations as part of a library, where consumers won't have your `migrations/` folder on disk.

```go
import (
    "embed"
    "io/fs"

    "github.com/md-talim/vow"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func newMigrator(pool *pgxpool.Pool) (*vow.Migrator, error) {
    // //go:embed retains the migrations/ directory in the resulting
    // fs.FS. Root it with fs.Sub so Vow sees the .sql files directly.
    rooted, err := fs.Sub(migrationFS, "migrations")
    if err != nil {
        return nil, err
    }
    return vow.New(pool, rooted)
}
```

Vow always assumes the `fs.FS` it's given is already rooted at the migrations directory; it never does subdirectory resolution internally. `os.DirFS("./migrations")` is already rooted correctly; an `embed.FS` from `//go:embed migrations/*.sql` is not, and needs `fs.Sub` first. This keeps the rooting decision in one place at construction time, rather than repeated as a directory argument on every call.

## Command Line

Vow ships with a minimal CLI, `vow`, for running migrations from the terminal. It is handy for local development and test teardown, where running migrations by hand beats embedding Vow in a throwaway program.

Install it with:

```bash
go install github.com/md-talim/vow/cmd/vow@latest
```

Usage:

```text
vow <command> [flags]

Commands:
  up    Apply all pending migrations
  down  Roll back the N most recently applied migrations (default 1)
```

`up` applies every pending migration and prints a summary:

```bash
DATABASE_URL=postgres://user:pass@localhost:5432/mydb vow up
# applied 2, skipped 0, took 45ms
```

`down` rolls back the N most recently applied migrations in reverse order. N defaults to 1:

```bash
vow down      # roll back the single most recent migration
vow down 3    # roll back the three most recent migrations
```

Both commands accept the same flags:

- `--database-url` PostgreSQL connection URL. Defaults to `$DATABASE_URL`.
- `--migrations-dir` Path to the migrations directory. Defaults to `./migrations`.
- `--table-name` Name of the migration tracking table. Defaults to `schema_migrations`.
- `--lock-name` Name of the advisory lock. Defaults to `vow:migrations`.

Run `vow <command> -h` for command-specific help. `vow` exits with status 0 on success, 1 on failure, and 130 if interrupted.

## Migration File Format

Migration files must be named using a numeric prefix to ensure ordering, paired with matching `.up.sql` and `.down.sql` files:

- `000001_create_users.up.sql`
- `000001_create_users.down.sql`
- `000002_add_email_index.up.sql`
- `000002_add_email_index.down.sql`

`New()` validates the migrations directory up front:

- Every `.up.sql` must have a matching `.down.sql`, and vice versa.
- Filenames must match the `NNNNNN_name.up.sql` / `NNNNNN_name.down.sql` pattern.
- Subdirectories are not allowed inside the migrations directory.

A malformed directory fails at construction time, not later when a migration or rollback is actually attempted.

## Design Notes

- **Applied migrations are checksummed, not just tracked by filename.** Every `.up.sql` file's SHA-256 hash is stored alongside its version at apply time. On every `Up` or `Down` run, before anything else happens, Vow re-reads every already-applied file and compares hashes: if a file was edited or deleted after being applied, the run fails immediately, before touching the database further.
- **Down migrations are not checksummed.** When a migration is rolled back, Vow deletes its tracking row. Because no permanent record of the rollback exists, a checksum is unnecessary; there is no stored state left to drift. This is deliberate: rollbacks often happen _because_ a migration needs to change, so down-files are intentionally left free to edit.
- **Down migrations are best-effort, not guaranteed-reversible.** A `.down.sql` file can undo a schema _shape_ change, but not necessarily recover data lost by the corresponding `.up.sql` (e.g. a dropped column). Write and review down migrations with that limitation in mind.
- **`Up` never calls `Down` automatically.** If a migration fails partway through a deploy, Vow fails loudly and stops rather than attempting an automatic rollback; an already-failed state is the wrong moment for the tool to attempt a risky, unverified undo. That decision is left to a human.

## Contributing

Contributions are welcome! Please feel free to open issues or submit pull requests for any improvements or bug fixes.
