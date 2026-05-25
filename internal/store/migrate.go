package store

import (
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed schema.sql
var schemaSQL string

//go:embed views.sql
var viewsSQL string

// migrations are applied in order; applying migrations[i] sets user_version to
// i+1. Forward-only: never edit a released migration string — append a new one.
var migrations = []string{
	schemaSQL + "\n" + viewsSQL,
}

// SchemaVersion is the latest schema version this build knows how to produce.
// A read-only reader compares it against the DB's PRAGMA user_version to decide
// whether to trust local data or fall back to the live API.
var SchemaVersion = len(migrations)

// Migrate brings db up to the latest schema using PRAGMA user_version as a
// forward-only counter. Each migration runs in its own transaction. Idempotent:
// re-running on an up-to-date DB is a no-op. Must run on a read-write connection.
func Migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	for i := version; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", i+1, err)
		}
		// PRAGMA can't be parameterized; i+1 is a trusted constant.
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("set user_version %d: %w", i+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", i+1, err)
		}
	}
	return nil
}
