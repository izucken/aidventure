# store

Opens the SQLite database and keeps its schema current. The driver is the
cgo-free `modernc.org/sqlite`, so the binary stays static.

## Migrations

Schema changes are goose migrations in `migrations/`, embedded into the binary
and applied every time the database is opened. There is no separate migrate
step. To add one, create the next sequential file:

    migrations/<NNNNN>_<name>.sql

with `-- +goose Up` and `-- +goose Down` sections. Never edit an applied migration.
