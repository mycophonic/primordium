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

package redact_test

import (
	"net/url"
	"testing"

	"github.com/mycophonic/primordium/network/internal/redact"
)

// TestURL: what is logged of a URL is its scheme, host and path, and nothing
// of its user information, query or fragment.
func TestURL(t *testing.T) {
	t.Parallel()

	for given, want := range map[string]string{
		"https://host.example/path":                                   "https://host.example/path",
		"https://user:secret@host.example:8443/path":                  "https://host.example:8443/path",
		"https://host.example/blob?X-Amz-Signature=secret&x=1#secret": "https://host.example/blob",
		"https://host.example/a%2Fb?token=secret":                     "https://host.example/a%2Fb",
		"https://host.example":                                        "https://host.example",
	} {
		parsed, err := url.Parse(given)
		if err != nil {
			t.Fatal(err)
		}

		if got := redact.URL(parsed); got != want {
			t.Errorf("URL(%q) = %q, want %q", given, got, want)
		}
	}

	if got := redact.URL(nil); got != "" {
		t.Errorf("URL(nil) = %q, want nothing", got)
	}
}
