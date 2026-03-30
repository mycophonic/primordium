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

package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite" // Pure-Go SQLite driver for tests.

	"github.com/mycophonic/primordium/db/sqlite"
	"github.com/mycophonic/primordium/filesystem"
)

const testDriver = "sqlite"

func openTestDB(t *testing.T) *sqlite.DB {
	t.Helper()

	ctx := context.Background()

	db, err := sqlite.OpenWithDriver(ctx, testDriver, ":memory:", sqlite.PragmasReadWrite)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = db.Close() })

	if err := db.ExecStatements(ctx, "CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatal(err)
	}

	return db
}

func TestOpen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	db, err := sqlite.OpenWithDriver(ctx, testDriver, ":memory:", sqlite.PragmasReadOnly)
	if err != nil {
		t.Fatal(err)
	}

	if db.Conn() == nil {
		t.Fatal("Conn() returned nil")
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenInvalidDriver(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	_, err := sqlite.OpenWithDriver(ctx, "nonexistent_driver", ":memory:", sqlite.Pragmas{})
	if err == nil {
		t.Fatal("expected error for invalid driver")
	}

	if !errors.Is(err, sqlite.ErrOpen) {
		t.Fatalf("expected base.ErrOpen, got: %v", err)
	}
}

func TestOpenFailingPragma(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	pragmas := sqlite.Pragmas{
		Statements: []string{"NOT VALID SQL"},
	}

	_, err := sqlite.OpenWithDriver(ctx, testDriver, ":memory:", pragmas)
	if err == nil {
		t.Fatal("expected error for invalid pragma")
	}

	if !errors.Is(err, sqlite.ErrOpen) {
		t.Fatalf("expected base.ErrOpen, got: %v", err)
	}
}

func TestOpenMaxConns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	pragmas := sqlite.Pragmas{MaxConns: 3}

	db, err := sqlite.OpenWithDriver(ctx, testDriver, ":memory:", pragmas)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = db.Close() }()

	if got := db.Conn().Stats().MaxOpenConnections; got != 3 {
		t.Fatalf("expected MaxOpenConnections=3, got %d", got)
	}
}

func TestSetGetMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t)

	if err := db.SetMetadata(ctx, "version", "42"); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetMetadata(ctx, "version")
	if err != nil {
		t.Fatal(err)
	}

	if got != "42" {
		t.Fatalf("expected %q, got %q", "42", got)
	}
}

func TestSetMetadataOverwrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t)

	if err := db.SetMetadata(ctx, "key", "first"); err != nil {
		t.Fatal(err)
	}

	if err := db.SetMetadata(ctx, "key", "second"); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetMetadata(ctx, "key")
	if err != nil {
		t.Fatal(err)
	}

	if got != "second" {
		t.Fatalf("expected %q, got %q", "second", got)
	}
}

func TestGetMetadataMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t)

	got, err := db.GetMetadata(ctx, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}

	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestExecStatements(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t)

	err := db.ExecStatements(ctx, `
		CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT);
		INSERT INTO items (id, name) VALUES (1, 'alpha');
		INSERT INTO items (id, name) VALUES (2, 'beta')
	`)
	if err != nil {
		t.Fatal(err)
	}

	var count int
	if err := db.Conn().QueryRowContext(ctx, "SELECT count(*) FROM items").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Fatalf("expected 2 rows, got %d", count)
	}
}

func TestExecStatementsAtomicity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t)

	if err := db.ExecStatements(ctx, "CREATE TABLE items (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}

	err := db.ExecStatements(ctx, "INSERT INTO items (id) VALUES (1); NOT VALID SQL")
	if err == nil {
		t.Fatal("expected error")
	}

	if !errors.Is(err, sqlite.ErrWrite) {
		t.Fatalf("expected base.ErrWrite, got: %v", err)
	}

	var count int
	if err := db.Conn().QueryRowContext(ctx, "SELECT count(*) FROM items").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatalf("expected 0 rows after rollback, got %d", count)
	}
}

func TestExecStatementsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t)

	if err := db.ExecStatements(ctx, ""); err != nil {
		t.Fatalf("expected no error for empty input, got: %v", err)
	}
}

func TestVacuumInto(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()

	srcPath := filepath.Join(dir, "source.db")
	destPath := filepath.Join(dir, "dest.db")

	db, err := sqlite.OpenWithDriver(ctx, testDriver, srcPath, sqlite.PragmasReadWrite)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Conn().ExecContext(ctx, "CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Conn().ExecContext(ctx, "INSERT INTO test (id, name) VALUES (1, 'hello')")
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if err := sqlite.VacuumInto(ctx, testDriver, srcPath, destPath); err != nil {
		t.Fatal(err)
	}

	dest, err := sqlite.OpenWithDriver(ctx, testDriver, destPath, sqlite.PragmasReadOnly)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = dest.Close() }()

	var name string

	err = dest.Conn().QueryRowContext(ctx, "SELECT name FROM test WHERE id = 1").Scan(&name)
	if err != nil {
		t.Fatal(err)
	}

	if name != "hello" {
		t.Fatalf("expected %q, got %q", "hello", name)
	}
}

func TestSetMetadataNoTable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Open without creating the metadata table.
	db, err := sqlite.OpenWithDriver(ctx, testDriver, ":memory:", sqlite.Pragmas{})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = db.Close() })

	err = db.SetMetadata(ctx, "key", "value")
	if err == nil {
		t.Fatal("expected error when metadata table does not exist")
	}

	if !errors.Is(err, sqlite.ErrWrite) {
		t.Fatalf("expected ErrWrite, got: %v", err)
	}
}

func TestGetMetadataNoTable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Open without creating the metadata table.
	db, err := sqlite.OpenWithDriver(ctx, testDriver, ":memory:", sqlite.Pragmas{})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = db.Close() })

	_, err = db.GetMetadata(ctx, "key")
	if err == nil {
		t.Fatal("expected error when metadata table does not exist")
	}

	if !errors.Is(err, sqlite.ErrRead) {
		t.Fatalf("expected ErrRead, got: %v", err)
	}
}

func TestExecStatementsWhitespace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t)

	if err := db.ExecStatements(ctx, "   \t\n  "); err != nil {
		t.Fatalf("expected no error for whitespace-only input, got: %v", err)
	}
}

func TestExecStatementsCancelledContext(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := db.ExecStatements(ctx, "CREATE TABLE t (id INTEGER)")
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}

	if !errors.Is(err, sqlite.ErrWrite) {
		t.Fatalf("expected ErrWrite, got: %v", err)
	}
}

func TestVacuumIntoExistingDest(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()

	srcPath := filepath.Join(dir, "source.db")
	destPath := filepath.Join(dir, "dest.db")

	db, err := sqlite.OpenWithDriver(ctx, testDriver, srcPath, sqlite.PragmasReadWrite)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if err := filesystem.WriteFile(destPath, []byte("existing"), filesystem.FilePermissionsDefault); err != nil {
		t.Fatal(err)
	}

	err = sqlite.VacuumInto(ctx, testDriver, srcPath, destPath)
	if err == nil {
		t.Fatal("expected error when dest already exists")
	}

	if !errors.Is(err, sqlite.ErrWrite) {
		t.Fatalf("expected base.ErrWrite, got: %v", err)
	}
}
