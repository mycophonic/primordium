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

// The bounded check: every preset's pool size, every pool size with a pragma
// set on every connection, and the prepared fallback for a driver whose
// connections have no ExecContext.

import (
	"database/sql"
	"database/sql/driver"
	"path/filepath"
	"strconv"
	"testing"

	"gotest.tools/v3/assert"
	msqlite "modernc.org/sqlite"

	"github.com/mycophonic/primordium/db/sqlite"
)

// TestPresetPoolSizes: each preset pins its pool as it says.
func TestPresetPoolSizes(t *testing.T) {
	t.Parallel()

	presets := map[string]sqlite.Pragmas{
		"ReadOnly": sqlite.PragmasReadOnly, "ReadWrite": sqlite.PragmasReadWrite,
		"Import": sqlite.PragmasImport, "Vacuum": sqlite.PragmasVacuum,
	}

	for name, preset := range presets {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db, err := sqlite.OpenWithDriver(t.Context(), testDriver, filepath.Join(t.TempDir(), "p.db"), preset)
			assert.NilError(t, err)

			t.Cleanup(func() { _ = db.Close() })

			assert.Equal(t, db.Conn().Stats().MaxOpenConnections, 1)
		})
	}
}

// TestBoundedPools: for every pool size, every connection the pool opens has
// the pragmas; the pool size is what MaxConns said, unlimited for zero.
func TestBoundedPools(t *testing.T) {
	t.Parallel()

	for _, size := range []uint{0, 1, 2, 3, 5} {
		t.Run(strconv.FormatUint(uint64(size), 10), func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			db, err := sqlite.OpenWithDriver(ctx, testDriver, filepath.Join(t.TempDir(), "pool.db"), sqlite.Pragmas{
				MaxConns:   size,
				Statements: []string{"PRAGMA busy_timeout = 4321", "PRAGMA cache_size = -1234"},
			})
			assert.NilError(t, err)

			t.Cleanup(func() { _ = db.Close() })

			assert.Equal(t, db.Conn().Stats().MaxOpenConnections, int(size)) // 0 is database/sql's unlimited

			open := int(size)
			if size == 0 {
				open = 4 // unlimited: a few
			}

			conns := make([]*sql.Conn, 0, open)

			for range open {
				conn, err := db.Conn().Conn(ctx)
				assert.NilError(t, err)

				conns = append(conns, conn)
			}

			t.Cleanup(func() {
				for _, conn := range conns {
					_ = conn.Close()
				}
			})

			for i, conn := range conns {
				assert.Equal(t, pragma(ctx, t, conn, "busy_timeout"), "4321", "connection %d", i)
				assert.Equal(t, pragma(ctx, t, conn, "cache_size"), "-1234", "connection %d", i)
			}
		})
	}
}

// noExecDriver is the pure-Go driver with connections that have no
// ExecContext, so the pragmas can only go through a prepared statement.
type noExecDriver struct{}

type noExecConn struct {
	inner driver.Conn
}

func (noExecDriver) Open(name string) (driver.Conn, error) {
	conn, err := (&msqlite.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}

	return &noExecConn{inner: conn}, nil
}

func (c *noExecConn) Prepare(query string) (driver.Stmt, error) {
	return c.inner.Prepare(query)
}

func (c *noExecConn) Close() error { return c.inner.Close() }

func (c *noExecConn) Begin() (driver.Tx, error) {
	return c.inner.Begin() //nolint:staticcheck // SA1019: the one Begin a driver.Conn must have
}

//nolint:gochecknoinits // a test driver is registered once, as drivers are
func init() {
	sql.Register("sqlite-noexec", noExecDriver{})
}

// TestPragmasThroughPreparedStatements: a driver without ExecContext still
// gets every pragma on every connection, prepared.
func TestPragmasThroughPreparedStatements(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := sqlite.OpenWithDriver(ctx, "sqlite-noexec", filepath.Join(t.TempDir(), "p.db"), sqlite.Pragmas{
		MaxConns:   2,
		Statements: []string{"PRAGMA busy_timeout = 777"},
	})
	assert.NilError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	first, err := db.Conn().Conn(ctx)
	assert.NilError(t, err)

	t.Cleanup(func() { _ = first.Close() })

	second, err := db.Conn().Conn(ctx)
	assert.NilError(t, err)

	t.Cleanup(func() { _ = second.Close() })

	assert.Equal(t, pragma(ctx, t, first, "busy_timeout"), "777")
	assert.Equal(t, pragma(ctx, t, second, "busy_timeout"), "777")

	_, err = sqlite.OpenWithDriver(ctx, "sqlite-noexec", filepath.Join(t.TempDir(), "q.db"), sqlite.Pragmas{
		Statements: []string{"NOT VALID SQL"},
	})
	assert.ErrorIs(t, err, sqlite.ErrOpen)
}
