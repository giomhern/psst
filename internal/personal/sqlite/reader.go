// Package sqlite implements read-only access to the local Messages SQLite database.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // Register the pure-Go database/sql driver.
)

// Reader owns a read-only connection pool. Call Close when finished. The pool is
// private so callers cannot issue arbitrary SQL through the adapter.
type Reader struct{ db *sql.DB }

// Open opens an existing regular file using mode=ro and query_only on every
// connection. It never creates a missing database or changes its journal mode.
// Errors are for internal classification; do not print them in diagnostics.
func Open(ctx context.Context, path string) (*Reader, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("database path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("database must be a regular file")
	}

	// Construct a URI from a filesystem path, never interpret a path as a DSN.
	// Encoding prevents '?' or '#' in a filename from injecting SQLite options.
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	options := url.Values{"mode": {"ro"}, "_pragma": {
		"busy_timeout(250)", "query_only(1)", "trusted_schema(0)",
	}}
	uri.RawQuery = options.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, fmt.Errorf("configure SQLite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// sql.Open is lazy. Force a connection and verify the per-connection guard
	// before returning a usable reader.
	var queryOnly int
	if err := db.QueryRowContext(ctx, "PRAGMA query_only").Scan(&queryOnly); err != nil {
		return nil, errors.Join(fmt.Errorf("open read-only SQLite: %w", err), db.Close())
	}
	if queryOnly != 1 {
		return nil, errors.Join(errors.New("SQLite query-only guard is disabled"), db.Close())
	}
	return &Reader{db: db}, nil
}

// Close releases the reader's database connections. No checkpoint, migration,
// purge, or filesystem deletion is performed by the adapter.
func (r *Reader) Close() error { return r.db.Close() }
