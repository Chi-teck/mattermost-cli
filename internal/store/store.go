// Package store is the local SQLite cache layer backing mm's local-first reads.
//
// The sync daemon opens the database read-write as the single writer; CLI query
// commands open it read-only. WAL mode lets many short-lived readers run while
// the daemon writes.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Open opens the SQLite database at path. When readOnly is true the connection
// is opened in read-only mode for the `mm query` command; the daemon opens it
// read-write.
func Open(path string, readOnly bool) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"
	if readOnly {
		dsn += "&mode=ro"
	} else {
		dsn += "&mode=rwc&_pragma=journal_mode(wal)&_pragma=synchronous(normal)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %s: %w", path, err)
	}
	if readOnly {
		db.SetMaxOpenConns(4)
		// Fail fast if the DB is missing/unusable so callers fall back to live.
		if err := db.Ping(); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("open sqlite (ro) at %s: %w", path, err)
		}
		return db, nil
	}
	// Single writer connection serializes the daemon's writes and avoids
	// SQLITE_BUSY contention with itself.
	db.SetMaxOpenConns(1)
	if err := Migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
