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
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"

	"modernc.org/sqlite"
)

var errNoAuthorizer = errors.New("the driver's connection takes no authorizer")

// fence keeps a batch inside the transaction opened for it: SQLite's
// authorizer on the batch's connection refuses BEGIN, COMMIT and ROLLBACK
// while it is raised. It is lifted before the transaction ends, because the
// authorizer refuses the driver's own COMMIT and ROLLBACK as well.
type fence struct {
	conn    *sql.Conn
	tripped string // the first transaction statement refused
}

func (f *fence) raise() error {
	return f.set(func(action sqlite.AuthorizerActionCode, operation, _, _, _ string) sqlite.AuthorizerReturnCode {
		if action != sqlite.AuthTransaction {
			return sqlite.AuthorizerOK
		}

		if f.tripped == "" {
			f.tripped = operation
		}

		return sqlite.AuthorizerDeny
	})
}

func (f *fence) lift() error {
	return f.set(nil)
}

// discard has the pool drop the physical connection: a policy that could not
// be removed stays on it and refuses its transactions. Only once no
// transaction is open on the sql.Conn: the discard takes the lock an open
// one holds.
func (f *fence) discard() {
	_ = f.conn.Raw(func(any) error { return driver.ErrBadConn })
}

func (f *fence) set(authorizer sqlite.AuthorizerFn) error {
	err := f.conn.Raw(func(driverConn any) error {
		registerer, ok := driverConn.(sqlite.AuthorizerRegisterer)
		if !ok {
			return errNoAuthorizer
		}

		if err := registerer.RegisterAuthorizer(authorizer); err != nil {
			return fmt.Errorf("authorizer: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("connection: %w", err)
	}

	return nil
}
