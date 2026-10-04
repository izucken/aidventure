// Package store opens the application's SQLite database and keeps its schema current.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // cgo-free driver, keeps the binary static
)

// Migrations are plain SQL in goose format, applied in file-name order.
//
//go:embed migrations/*.sql
var migrations embed.FS

// Open opens or creates the database at path and applies pending migrations.
// WAL keeps reads from blocking on writes; foreign keys are off in SQLite unless asked for.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return db, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	src, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, db, src)
	if err != nil {
		return err
	}
	_, err = p.Up(ctx)
	return err
}
