/*
   Copyright Mycophonic.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Pragmas holds SQLite PRAGMA statements and connection pool settings
// to apply when opening a database.
type Pragmas struct {
	Statements []string
	MaxConns   int // 0 = database/sql default (unlimited).
}

// PragmasReadOnly configures SQLite for pure read performance.
//
//nolint:gochecknoglobals
var PragmasReadOnly = Pragmas{
	MaxConns: 1,
	Statements: []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA cache_size = -64000", // 64 MB
		"PRAGMA busy_timeout = 5000",
	},
}

// PragmasReadWrite configures SQLite for mostly read, occasional write model.
// Read performance and data integrity are paramount, write performance is not a concern.
// Uses synchronous=FULL (not NORMAL) to guarantee durability on power failure.
//
//nolint:gochecknoglobals
var PragmasReadWrite = Pragmas{
	MaxConns: 1,
	Statements: []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = FULL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA cache_size = -64000", // 64 MB
		"PRAGMA busy_timeout = 5000",
	},
}

// PragmasImport configures aggressive settings for expendable work databases.
// Disables journaling and fsync for maximum write throughput. The work database
// is disposable, so crash safety is traded for speed.
//
// Pins the connection pool to a single connection so that all subsequent
// operations (imports, indexing) share the same pragmas.
//
// Requires up to 4 GB RAM for the page cache and 8 GB address space for mmap.
//
//nolint:gochecknoglobals
var PragmasImport = Pragmas{
	MaxConns: 1,
	Statements: []string{
		"PRAGMA page_size = 16384",  // 16 KB (must be before first write)
		"PRAGMA auto_vacuum = NONE", // no page reclamation overhead
		"PRAGMA foreign_keys = OFF",
		"PRAGMA journal_mode = OFF",
		"PRAGMA synchronous = OFF",
		"PRAGMA locking_mode = EXCLUSIVE",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA threads = 8",            // parallel sorting for CREATE INDEX
		"PRAGMA cache_size = -4194304",  // 4 GB
		"PRAGMA mmap_size = 8589934592", // 8 GB
	},
}

// PragmasVacuum configures settings optimized for VACUUM operations.
// Opening a WAL-mode database with journal_mode=OFF implicitly checkpoints the
// existing WAL, so no separate wal_checkpoint call is needed afterward.
//
// Requires up to 4 GB RAM for the page cache and 8 GB address space for mmap.
//
//nolint:gochecknoglobals
var PragmasVacuum = Pragmas{
	MaxConns: 1,
	Statements: []string{
		"PRAGMA journal_mode = OFF",
		"PRAGMA synchronous = OFF",
		"PRAGMA locking_mode = EXCLUSIVE",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA cache_size = -4194304",  // 4 GB
		"PRAGMA mmap_size = 8589934592", // 8 GB
	},
}

// VacuumInto opens the database at path with vacuum-optimized pragmas,
// compacts it into a new file at destPath, and closes the connection.
// destPath must not already exist; SQLite will refuse to overwrite an
// existing file. Callers that want in-place compaction should vacuum
// into a temporary file and rename it over the original.
func VacuumInto(ctx context.Context, driver, path, destPath string) error {
	store, err := OpenWithDriver(ctx, driver, path, PragmasVacuum)
	if err != nil {
		return err
	}

	defer func() { _ = store.Close() }()

	if _, err := store.conn.ExecContext(ctx, "VACUUM INTO ?", destPath); err != nil {
		return fmt.Errorf("%w: vacuum into: %w", ErrWrite, err)
	}

	return nil
}

// DB wraps a SQLite database connection with performance pragmas.
type DB struct {
	conn *sql.DB
}

// OpenWithDriver opens or creates a SQLite database at path using the named driver
// and applies the given pragma configuration.
func OpenWithDriver(ctx context.Context, driver, path string, pragmas Pragmas) (*DB, error) {
	conn, err := sql.Open(driver, path)
	if err != nil {
		return nil, fmt.Errorf("%w: open: %w", ErrOpen, err)
	}

	if pragmas.MaxConns > 0 {
		conn.SetMaxOpenConns(pragmas.MaxConns)
	}

	for _, pragma := range pragmas.Statements {
		if _, err := conn.ExecContext(ctx, pragma); err != nil {
			_ = conn.Close()

			return nil, fmt.Errorf("%w: %s: %w", ErrOpen, pragma, err)
		}
	}

	return &DB{conn: conn}, nil
}

// Conn returns the underlying *sql.DB for domain-specific operations.
func (store *DB) Conn() *sql.DB {
	return store.conn
}

// Close closes the database connection.
func (store *DB) Close() error {
	if err := store.conn.Close(); err != nil {
		return fmt.Errorf("%w: close: %w", ErrClose, err)
	}

	return nil
}

// SetMetadata inserts or replaces a key-value pair in the metadata table.
func (store *DB) SetMetadata(ctx context.Context, key, value string) error {
	_, err := store.conn.ExecContext(
		ctx,
		"INSERT OR REPLACE INTO metadata (key, value) VALUES (?, ?)",
		key, value,
	)
	if err != nil {
		return fmt.Errorf("%w: set metadata %s: %w", ErrWrite, key, err)
	}

	return nil
}

// GetMetadata returns the value for key from the metadata table.
// Returns ("", nil) when the key does not exist.
func (store *DB) GetMetadata(ctx context.Context, key string) (string, error) {
	var value string

	err := store.conn.QueryRowContext(
		ctx,
		"SELECT value FROM metadata WHERE key = ?",
		key,
	).Scan(&value)

	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	if err != nil {
		return "", fmt.Errorf("%w: get metadata %s: %w", ErrRead, key, err)
	}

	return value, nil
}

// ExecStatements splits a semicolon-delimited SQL string and executes each
// statement atomically inside a transaction.
func (store *DB) ExecStatements(ctx context.Context, raw string) error {
	txn, err := store.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin: %w", ErrWrite, err)
	}

	for _, stmt := range splitStatements(raw) {
		if _, err := txn.ExecContext(ctx, stmt); err != nil {
			_ = txn.Rollback()

			return fmt.Errorf("%w: exec [%.80s]: %w", ErrWrite, stmt, err)
		}
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("%w: commit: %w", ErrWrite, err)
	}

	return nil
}

// splitStatements splits a semicolon-delimited SQL file into individual statements.
func splitStatements(raw string) []string {
	parts := strings.Split(raw, ";")
	stmts := make([]string, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			stmts = append(stmts, trimmed)
		}
	}

	return stmts
}
