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
	// DefaultSSHConfig provides secure cryptographic defaults for use in ssh config.
	// Note: this WILL break on ancient / misconfigured systems.
	DefaultSSHConfig = ssh.Config{
		// Modern key exchanges only (Curve25519-based)
		KeyExchanges: []string{
			"curve25519-sha256",
			"curve25519-sha256@libssh.org",
		},
		// AEAD ciphers only - no CBC mode
		Ciphers: []string{
			"chacha20-poly1305@openssh.com",
			"aes256-gcm@openssh.com",
			"aes128-gcm@openssh.com",
		},
		// Encrypt-then-MAC only
		MACs: []string{
			"hmac-sha2-256-etm@openssh.com",
			"hmac-sha2-512-etm@openssh.com",
		},
	}

	// DefaultSSHHostKeyAlgorithms provides the list of algorithms we support for host keys.
	// Note: this WILL break on ancient / misconfigured systems.
	DefaultSSHHostKeyAlgorithms = []string{
		ssh.KeyAlgoED25519,
	}

	// DefaultSSHConnectionTimeout is the timeout for ssh connections.
	DefaultSSHConnectionTimeout = 30 * time.Second

	// DefaultSSHKeepaliveTimeout is how long to wait for a keepalive response before
	// considering the connection dead.
	DefaultSSHKeepaliveTimeout = 15 * time.Second

	// DefaultIdentityFiles defines the well-known private key we might consider.
	DefaultIdentityFiles = []string{
		// "~/.ssh/id_rsa",
		// "~/.ssh/id_ecdsa",
		// "~/.ssh/id_ecdsa_sk",
		"~/.ssh/id_ed25519",
		"~/.ssh/id_ed25519_sk",
	}
)
