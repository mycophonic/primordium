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

package network_test

import (
	"testing"

	"golang.org/x/crypto/ssh"
	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/network"
)

// TestSSHClientConfig: the SSH posture is the one stated, Curve25519 key
// exchange, AEAD ciphers, encrypt-then-MAC, ed25519 host keys, and every
// call hands out its own copy.
func TestSSHClientConfig(t *testing.T) {
	t.Parallel()

	cfg := network.GetClientConfig()

	assert.DeepEqual(t, cfg.KeyExchanges, []string{"curve25519-sha256", "curve25519-sha256@libssh.org"})
	assert.DeepEqual(
		t,
		cfg.Ciphers,
		[]string{"chacha20-poly1305@openssh.com", "aes256-gcm@openssh.com", "aes128-gcm@openssh.com"},
	)
	assert.DeepEqual(t, cfg.MACs, []string{"hmac-sha2-256-etm@openssh.com", "hmac-sha2-512-etm@openssh.com"})
	assert.DeepEqual(t, cfg.HostKeyAlgorithms, []string{ssh.KeyAlgoED25519})
	assert.DeepEqual(t, cfg.IdentityFiles, []string{"~/.ssh/id_ed25519", "~/.ssh/id_ed25519_sk"})
	assert.Assert(t, cfg.Timeout > 0)
	assert.Assert(t, cfg.KeepAliveTimeout > 0)
	assert.Assert(t, cfg.HostKeyCallback == nil, "host key verification is the caller's")
	assert.Assert(t, len(cfg.Auth) == 0, "authentication is the caller's")

	other := network.GetClientConfig()
	other.Ciphers[0] = "none"
	other.IdentityFiles[0] = ""

	assert.Equal(
		t,
		network.GetClientConfig().Ciphers[0],
		"chacha20-poly1305@openssh.com",
		"a caller's edit reached the defaults",
	)
	assert.Equal(
		t,
		network.GetClientConfig().IdentityFiles[0],
		"~/.ssh/id_ed25519",
		"a caller's edit reached the defaults",
	)
}
