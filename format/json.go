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

package format

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/mycophonic/primordium/fault"
)

// JSON renders the entries as one indented array.
type JSON struct{}

// PrintAll writes all data entries as a JSON array to the writer: an empty
// array for no entries, and ErrInvalidArgument for a value JSON cannot carry,
// such as a NaN.
func (*JSON) PrintAll(data []*Data, writer io.Writer) error {
	if data == nil {
		data = []*Data{}
	}

	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: %w", fault.ErrInvalidArgument, err)
	}

	if _, err = writer.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("%w: %w", fault.ErrWriteFailure, err)
	}

	return nil
}
