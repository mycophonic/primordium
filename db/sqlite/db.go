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
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/mycophonic/primordium/bytesize"
)

// Pragmas holds SQLite PRAGMA statements and connection pool settings
// to apply when opening a database.
//
// A PRAGMA is a connection's setting, not the database's, so the statements
// run on every connection the pool opens, in order. Order matters: some
// pragmas (page_size, auto_vacuum) must be set before the first write to the
// database, so they must appear before pragmas that implicitly write
// (journal_mode).
type Pragmas struct {
	Statements []string
	MaxConns   uint // 0 = database/sql default (unlimited).
}

// defaultCacheSize is the page cache of the read-only and read-write presets.
const defaultCacheSize = 64 * bytesize.MiB

// sizePragma sets pragma to size bytes.
func sizePragma(pragma string, size int64) string {
	return fmt.Sprintf("PRAGMA %s = %d", pragma, size)
}

// cacheSizePragma sets the page cache to size bytes. The value is negated on
// purpose: a positive cache_size counts pages, a negative one counts KiB.
func cacheSizePragma(size int64) string {
	return fmt.Sprintf("PRAGMA cache_size = -%d", size/bytesize.KiB)
}

// PragmasReadOnly configures SQLite for pure read performance.
//
//nolint:gochecknoglobals // an exported preset, copied by value
var PragmasReadOnly = Pragmas{
	MaxConns: 1,
	Statements: []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		cacheSizePragma(defaultCacheSize),
		"PRAGMA busy_timeout = 5000",
	},
}

// PragmasReadWrite configures SQLite for mostly read, occasional write model.
// Read performance and data integrity are paramount, write performance is not a concern.
// Uses synchronous=FULL (not NORMAL) to guarantee durability on power failure.
//
//nolint:gochecknoglobals // an exported preset, copied by value
var PragmasReadWrite = Pragmas{
	MaxConns: 1,
	Statements: []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = FULL",
		"PRAGMA foreign_keys = ON",
		cacheSizePragma(defaultCacheSize),
		"PRAGMA busy_timeout = 5000",
	},
}

// PragmasImport configures aggressive settings for expendable work databases.
// Disables journaling and fsync for maximum write throughput. The work database
// is disposable, so crash safety is traded for speed. One connection: the
// exclusive lock would hold a second one off.
//
// Requires up to 4 GiB RAM for the page cache. The 8 GiB asked of mmap_size
// is capped at SQLite's compiled maximum, 2 GiB in the pure-Go and cgo drivers.
//
//nolint:gochecknoglobals // an exported preset, copied by value
var PragmasImport = Pragmas{
	MaxConns: 1,
	Statements: []string{
		// page_size and auto_vacuum must precede any pragma that writes
		// (e.g. journal_mode). Reordering these below journal_mode will
		// silently leave them at their defaults.
		sizePragma("page_size", 16*bytesize.KiB),
		"PRAGMA auto_vacuum = NONE", // no page reclamation overhead
		"PRAGMA foreign_keys = OFF",
		"PRAGMA journal_mode = OFF",
		"PRAGMA synchronous = OFF",
		"PRAGMA locking_mode = EXCLUSIVE",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA threads = 8", // parallel sorting for CREATE INDEX
		cacheSizePragma(4 * bytesize.GiB),
		sizePragma("mmap_size", 8*bytesize.GiB),
	},
}

// PragmasVacuum configures settings optimized for VACUUM operations.
// Opening a WAL-mode database with journal_mode=OFF implicitly checkpoints the
// existing WAL, so no separate wal_checkpoint call is needed afterward.
//
// Requires up to 4 GiB RAM for the page cache. The 8 GiB asked of mmap_size
// is capped at SQLite's compiled maximum, 2 GiB in the pure-Go and cgo drivers.
//
//nolint:gochecknoglobals // an exported preset, copied by value
var PragmasVacuum = Pragmas{
	MaxConns: 1,
	Statements: []string{
		"PRAGMA journal_mode = OFF",
		"PRAGMA synchronous = OFF",
		"PRAGMA locking_mode = EXCLUSIVE",
		"PRAGMA temp_store = MEMORY",
		cacheSizePragma(4 * bytesize.GiB),
		sizePragma("mmap_size", 8*bytesize.GiB),
	},
}

// VacuumInto opens the database at path with vacuum-optimized pragmas,
// compacts it into a new file at destPath, and closes the connection.
// destPath must not already exist; SQLite will refuse to overwrite an
// existing file. Callers that want in-place compaction should vacuum
// into a temporary file and rename it over the original.
func VacuumInto(ctx context.Context, driverName, path, destPath string) (err error) {
	store, err := OpenWithDriver(ctx, driverName, path, PragmasVacuum)
	if err != nil {
		return err
	}

	defer func() {
		closeErr := store.Close()
		if err == nil {
			err = closeErr
		}
	}()

	if _, err = store.conn.ExecContext(ctx, "VACUUM INTO ?", destPath); err != nil {
		return fmt.Errorf("%w: vacuum into: %w", ErrWrite, err)
	}

	return nil
}

// DB wraps a SQLite database connection with performance pragmas.
type DB struct {
	conn *sql.DB
}

// OpenWithDriver opens or creates a SQLite database at path using the named
// driver and applies the pragma configuration to every connection the pool
// opens. The first connection is opened here, so a path or a pragma the
// driver refuses fails the open.
func OpenWithDriver(ctx context.Context, driverName, path string, pragmas Pragmas) (*DB, error) {
	// sql.Open resolves the name without connecting; the driver.Driver is what
	// the connector needs.
	named, err := sql.Open(driverName, path)
	if err != nil {
		return nil, fmt.Errorf("%w: open: %w", ErrOpen, err)
	}

	_ = named.Close() // nothing was connected

	conn := sql.OpenDB(&connector{base: named.Driver(), dsn: path, statements: pragmas.Statements})

	if pragmas.MaxConns > 0 {
		conn.SetMaxOpenConns(int(min(pragmas.MaxConns, uint(math.MaxInt))))
	}

	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("%w: %w", ErrOpen, err)
	}

	return &DB{conn: conn}, nil
}

// connector opens the driver's connections and runs the pragma statements on
// each: database/sql opens as many as the pool needs, and replaces one it
// finds bad, and a pragma run once through the pool reached one of them only.
type connector struct {
	base       driver.Driver
	dsn        string
	statements []string
}

func (c *connector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.base.Open(c.dsn)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	for _, statement := range c.statements {
		if err := execute(ctx, conn, statement); err != nil {
			_ = conn.Close()

			return nil, fmt.Errorf("%s: %w", statement, err)
		}
	}

	return conn, nil
}

func (c *connector) Driver() driver.Driver {
	return c.base
}

// execute runs one statement on a raw connection: through ExecerContext when
// the driver has it and takes the statement, else prepared, as database/sql
// itself falls back.
func execute(ctx context.Context, conn driver.Conn, statement string) error {
	if execer, ok := conn.(driver.ExecerContext); ok {
		_, err := execer.ExecContext(ctx, statement, nil)
		if !errors.Is(err, driver.ErrSkip) {
			return err //nolint:wrapcheck // Connect names the statement
		}
	}

	stmt, err := conn.Prepare(statement)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}

	defer func() { _ = stmt.Close() }() // a statement run once; its close error carries nothing

	//nolint:staticcheck // SA1019: driver.Stmt.Exec is the fallback database/sql itself keeps
	if _, err := stmt.Exec(nil); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
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

// ExecStatements executes a semicolon-delimited SQL string atomically
// inside a transaction.
func (store *DB) ExecStatements(ctx context.Context, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	txn, err := store.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin: %w", ErrWrite, err)
	}

	if _, err := txn.ExecContext(ctx, raw); err != nil {
		_ = txn.Rollback() // Intentional: rollback failure doesn't change what the caller should do.

		return fmt.Errorf("%w: exec: %w", ErrWrite, err)
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("%w: commit: %w", ErrWrite, err)
	}

	return nil
}
