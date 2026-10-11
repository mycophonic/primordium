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
	"errors"
	"testing"

	"github.com/mycophonic/primordium/db/sqlite"
)

// FuzzExecStatements: whatever the text, ExecStatements runs it or refuses
// it as ErrWrite, and a refusal leaves the database as it was.
func FuzzExecStatements(f *testing.F) {
	for _, seed := range []string{
		"", "   ", "CREATE TABLE t (id INTEGER)", "INSERT INTO metadata VALUES ('k', 'v'); INSERT INTO metadata VALUES ('k2', 'v2')",
		"INSERT INTO metadata VALUES ('k', 'v'); NOT SQL", "SELECT 1;", "INSERT INTO metadata VALUES ('a;b', ';');", "--comment",
		"BEGIN; INSERT INTO metadata VALUES ('x', 'y'); COMMIT",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		ctx := t.Context()
		db := openTestDB(t)

		before := count(t, db)

		err := db.ExecStatements(ctx, raw)
		if err == nil {
			return
		}

		if !errors.Is(err, sqlite.ErrWrite) {
			t.Fatalf("ExecStatements(%q): %v is not ErrWrite", raw, err)
		}

		if after := count(t, db); after != before {
			t.Fatalf("ExecStatements(%q) failed and left %d rows, had %d", raw, after, before)
		}
	})
}

func count(t *testing.T, db *sqlite.DB) int {
	t.Helper()

	var n int
	if err := db.Conn().QueryRowContext(t.Context(), "SELECT count(*) FROM metadata").Scan(&n); err != nil {
		t.Fatal(err)
	}

	return n
}
