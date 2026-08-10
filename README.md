# Vow

Vow is a lightweight, opinionated migration runner for Go projects using `pgx` and PostgreSQL.

It is designed to be embedded directly into your application, ensuring your database schema is up-to-date before your service starts.

## Why Vow?

- **Zero Dependencies:** Only relies on `pgx/v5`.
- **Safety First:** Uses PostgreSQL advisory locks to ensure multiple service instances don't run migrations simultaneously.
- **Simple:** No configuration files or complex CLI tools. Just code.
- **Immutable:** Enforces strict validation that applied migrations cannot be renamed, deleted, or edited — checksums are verified on every run.
- **Reversible:** Every migration is paired with a `.down.sql` counterpart, validated up front.

## Getting Started

1. **Install:**

    ```bash
    go get github.com/md-talim/vow
    ```

2. **Usage:**
   Create a directory (e.g., `./migrations`) and add paired `.up.sql` / `.down.sql` files.

    ```go
        import (
            "context"
            "log"

            "github.com/md-talim/vow"
        )

        func main() {
            // ... initialize your pgxpool.Pool ...

            migrator, err := vow.New(dbPool, "./migrations")
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

- **Applied migrations are checksummed, not just tracked by filename.** Every `.up.sql` file's SHA-256 hash is stored alongside its version at apply time. On every `Up` or `Down` run, before anything else happens, Vow re-reads every already-applied file and compares hashes — if a file was edited or deleted after being applied, the run fails immediately, before touching the database further.
- **Down migrations are not checksummed.** Once a migration is rolled back, its tracking row is deleted — there's no longer a stored claim about what the down-migration did, so there's nothing for a checksum to protect against drifting from. This is deliberate: rollbacks often happen _because_ a migration needs to change, so down-files are intentionally left free to edit.
- **Down migrations are best-effort, not guaranteed-reversible.** A `.down.sql` file can undo a schema _shape_ change, but not necessarily recover data lost by the corresponding `.up.sql` (e.g. a dropped column). Write and review down migrations with that limitation in mind.
- **`Up` never calls `Down` automatically.** If a migration fails partway through a deploy, Vow fails loudly and stops rather than attempting an automatic rollback — an already-failed state is the wrong moment for the tool to attempt a risky, unverified undo. That decision is left to a human.
