# Vow

Vow is a lightweight, opinionated migration runner for Go projects using `pgx` and PostgreSQL.

It is designed to be embedded directly into your application, ensuring your database schema is up-to-date before your service starts.

## Why Vow?

- **Zero Dependencies:** Only relies on `pgx/v5`.
- **Safety First:** Uses PostgreSQL advisory locks to ensure multiple service instances don't run migrations simultaneously.
- **Simple:** No configuration files or complex CLI tools. Just code.
- **Immutable:** Enforces strict validation that applied migrations cannot be renamed or deleted.

## Getting Started

1. **Install:**

    ```bash
    go get github.com/md-talim/vow
    ```

2. **Usage:**
   Create a directory (e.g., `./migrations`) and add your `.sql` files with the format `000001_initial.sql`.

    ```go
    import (
        "context"
        "github.com/md-talim/vow"
    )

    func main() {
        // ... initialize your pgxpool.Pool ...

        migrator := vow.New(dbPool, "./migrations")

        if err := migrator.Run(context.Background()); err != nil {
            log.Fatalf("failed to migrate: %v", err)
        }
    }
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

Migration files must be named using a numeric prefix to ensure ordering:

- `000001_create_users.sql`
- `000002_add_email_index.sql`
