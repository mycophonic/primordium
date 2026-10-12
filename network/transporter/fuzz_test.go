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

package transporter_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/network/transporter"
)

// FuzzRetryAfter: whatever the Retry-After header says, a 503 carrying it is
// either retried once and answered, or given up on with the response's error
// and no retry; the client never hangs and never panics. Wall time is not
// the measure: under a loaded machine a 50 ms wait has taken over a second.
func FuzzRetryAfter(f *testing.F) {
	for _, seed := range []string{"", "0", "1", "-1", "9999999999999999999", "Wed, 21 Oct 2015 07:28:00 GMT", "soon", "0.5", "1e3", " 1", "1 "} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, header string) {
		const maxBackoff = 50 * time.Millisecond

		back := newBackend(t, func(w http.ResponseWriter, _ *http.Request, call int32) {
			if call == 1 {
				w.Header().Set("Retry-After", header)
				w.WriteHeader(http.StatusServiceUnavailable)

				return
			}

			w.WriteHeader(http.StatusOK)
		})

		client := newClient(t, transporter.Options{MaxRetries: 1, MaxBackoff: maxBackoff})

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()

		resp, err := doGet(ctx, t, client, back.url)
		if err == nil {
			_ = resp.Body.Close()
		}

		switch {
		case err == nil && back.calls.Load() == 2:
		case errors.Is(err, fault.ErrUnacceptableResponse) && back.calls.Load() == 1:
		default:
			t.Fatalf("Retry-After %q: err=%v after %d calls", header, err, back.calls.Load())
		}
	})
}
