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

package network

import (
	"time"

	"golang.org/x/crypto/ssh"
)

//nolint:gochecknoglobals
var (
	// defaultKeyExchanges provides modern key exchanges only (Curve25519-based).
	defaultKeyExchanges = []string{
		"curve25519-sha256",
		"curve25519-sha256@libssh.org",
	}

	// Ciphers are AEAD only, no CBC mode.
	defaultCiphers = []string{
		"chacha20-poly1305@openssh.com",
		"aes256-gcm@openssh.com",
		"aes128-gcm@openssh.com",
	}

	// MACs encrypt-then-MAC only.
	defaultMACs = []string{
		"hmac-sha2-256-etm@openssh.com",
		"hmac-sha2-512-etm@openssh.com",
	}

	// defaultSSHHostKeyAlgorithms provides the list of algorithms we support for host keys.
	// Note: this WILL break on ancient / misconfigured systems.
	defaultSSHHostKeyAlgorithms = []string{
		ssh.KeyAlgoED25519,
	}

	// defaultSSHConnectionTimeout is the timeout for ssh connections.
	defaultSSHConnectionTimeout = 30 * time.Second

	// defaultSSHKeepaliveTimeout is how long to wait for a keepalive response before
	// considering the connection dead.
	defaultSSHKeepaliveTimeout = 15 * time.Second

	// defaultIdentityFiles defines the well-known private key we might consider.
	defaultIdentityFiles = []string{
		// "~/.ssh/id_rsa",
		// "~/.ssh/id_ecdsa",
		// "~/.ssh/id_ecdsa_sk",
		"~/.ssh/id_ed25519",
		"~/.ssh/id_ed25519_sk",
	}
)

// ClientConfig extends ssh.ClientConfig with keepalive and identity file settings.
type ClientConfig struct {
	ssh.ClientConfig

	KeepAliveTimeout time.Duration

	IdentityFiles []string
}

// GetClientConfig returns a new SSH client configuration with hardened defaults.
func GetClientConfig() *ClientConfig {
	return &ClientConfig{
		ClientConfig: ssh.ClientConfig{
			Config: ssh.Config{
				KeyExchanges: append([]string{}, defaultKeyExchanges...),
				Ciphers:      append([]string{}, defaultCiphers...),
				MACs:         append([]string{}, defaultMACs...),
			},
			HostKeyAlgorithms: append([]string{}, defaultSSHHostKeyAlgorithms...),
			Timeout:           defaultSSHConnectionTimeout,
		},

		KeepAliveTimeout: defaultSSHKeepaliveTimeout,
		IdentityFiles:    append([]string{}, defaultIdentityFiles...),
	}
}
