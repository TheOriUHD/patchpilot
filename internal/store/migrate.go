package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Migrate applies embedded SQL migrations that have not yet run.
func (db *DB) Migrate(ctx context.Context, migrationFS fs.FS) error {
	if _, err := db.conn.ExecContext(ctx, `
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version    INTEGER PRIMARY KEY,
            name       TEXT NOT NULL,
            applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
        )
    `); err != nil {
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}

	migrations, err := collectMigrations(migrationFS)
	if err != nil {
		return err
	}
	for _, migration := range migrations {
		applied, err := db.migrationApplied(ctx, migration.version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := db.applyMigration(ctx, migrationFS, migration); err != nil {
			return err
		}
	}
	return nil
}

type migrationFile struct {
	version int
	name    string
}

func collectMigrations(migrationFS fs.FS) ([]migrationFile, error) {
	entries, err := fs.ReadDir(migrationFS, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	migrations := make([]migrationFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		versionText, _, found := strings.Cut(entry.Name(), "_")
		if !found {
			return nil, fmt.Errorf("migration %s is missing numeric prefix", entry.Name())
		}
		version, err := strconv.Atoi(versionText)
		if err != nil {
			return nil, fmt.Errorf("parse migration version %s: %w", entry.Name(), err)
		}
		migrations = append(migrations, migrationFile{version: version, name: entry.Name()})
	}

	sort.Slice(migrations, func(left int, right int) bool {
		return migrations[left].version < migrations[right].version
	})
	return migrations, nil
}

func (db *DB) migrationApplied(ctx context.Context, version int) (bool, error) {
	var exists int
	err := db.conn.QueryRowContext(ctx, "SELECT 1 FROM schema_migrations WHERE version = ?", version).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("check migration %03d: %w", version, err)
}

func (db *DB) applyMigration(ctx context.Context, migrationFS fs.FS, migration migrationFile) error {
	script, err := fs.ReadFile(migrationFS, migration.name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", migration.name, err)
	}

	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", migration.name, err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, string(script)); err != nil {
		return fmt.Errorf("apply migration %s: %w", migration.name, err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, name) VALUES (?, ?)", migration.version, migration.name); err != nil {
		return fmt.Errorf("record migration %s: %w", migration.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", migration.name, err)
	}
	tx = nil
	return nil
}
