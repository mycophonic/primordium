# Audit: `network` package

**Date:** 2026-03-29
**Scope:** `github.com/mycophonic/primordium/network`
**Files:** doc.go, http.go, ssh.go, http_test.go

---

## 1. Package purpose

Provides hardened HTTP and SSH transport defaults for all network operations across the mycophonic ecosystem. HTTP side: TLS 1.3 minimum, post-quantum hybrid key exchange (X25519MLKEM768), connection pool tuning, retryable status code classification, and a `RoundTripper` with auth header injection and retry logging. SSH side: Ed25519-only host keys, Curve25519 key exchange, AEAD-only ciphers, ETM-only MACs.

Used by 9 files across primordium and quark: app initialization, container registry operations (mTLS, custom CAs), SSH connection pooling, SSH client configuration, and CA trust management.

---

## 2. Correctness

No issues.

---

## 3. API fitness

### 3.1 `ClientConfig` embeds `ssh.ClientConfig` — clean

`ClientConfig` embeds `ssh.ClientConfig` directly, so callers get the full SSH client config plus extra fields (`KeepAliveTimeout`, `IdentityFiles`). Callers set `Auth` and `HostKeyCallback` on the embedded field. `GetClientConfig()` copies all slices, so callers can safely mutate their config without affecting defaults.

### 3.2 HTTP two-tier design — clean

`SetDefaults()` configures the global `http.DefaultTransport` (for callers using `http.DefaultClient`). `NewTransport()` returns an independent clone for callers needing custom TLS (mTLS, private CAs). `TokenType` defaults to `"Bearer"`. `RetryStatusCodes` is derived from `retryReasons` (single source of truth).

---

## 4. Organization

### 4.1 File structure — clean

- `doc.go`: Package documentation
- `http.go`: HTTP transport, `RoundTripper`, `SetDefaults`, `NewTransport` (178 lines)
- `ssh.go`: SSH defaults, `ClientConfig`, `GetClientConfig` (94 lines)
- `http_test.go`: HTTP tests (270 lines)

Clear separation of concerns. Each file has a single responsibility.

---

## 5. Test coverage

### 5.1 HTTP — good coverage (10 tests)

| Test                                         | What it covers                                 |
|----------------------------------------------|------------------------------------------------|
| `TestRoundTripper_InjectsAuthHeader`         | Auth header set when token present             |
| `TestRoundTripper_NoAuthWhenTokenEmpty`      | No header when token empty                     |
| `TestRoundTripper_LogsRetryableStatus`       | All 5 retry codes pass through without error   |
| `TestNewTransport_ClonesIndependently`       | Two clones don't share state                   |
| `TestRetryStatusCodes_ContainsExpectedCodes` | Retry code list has expected values            |
| `TestNewTransport_TLSMinVersionTLS13`        | TLS 1.3 minimum enforced                       |
| `TestNewTransport_TLSCurvePreferences`       | Post-quantum curve preferred, X25519 fallback  |
| `TestNewTransport_TimeoutConfiguration`      | All timeouts and pool settings non-zero        |
| `TestSetDefaults_ConfiguresDefaultTransport` | Global default is `*RoundTripper` with TLS 1.3 |
| `TestMain`                                   | Calls `SetDefaults()` before test suite        |

Well-structured: parallel where safe, non-parallel for global mutation, real `httptest.Server` for integration.

### 5.2 SSH — zero coverage

No tests for `GetClientConfig()` or any SSH defaults. At minimum:

- **P0:** `GetClientConfig()` returns non-nil with all fields populated
- **P0:** `HostKeyAlgorithms` contains only `ssh-ed25519`
- **P0:** Ciphers, key exchanges, MACs are non-empty and match expected values
- **P1:** `IdentityFiles` entries are Ed25519 variants
- **P1:** Timeouts are positive
- **P1:** Returned slices are independent copies (mutation doesn't affect defaults)

### 5.3 Missing HTTP test cases

- **P1:** `NewTransport()` before `SetDefaults()` — should panic
- **P2:** Request cloning — verify original request headers are not modified when token is set

---

## 6. Summary

| Area             | Rating  | Notes                                                          |
|------------------|---------|----------------------------------------------------------------|
| Correctness      | Good    | No issues                                                      |
| API fitness      | Good    | Clean embedding, safe slice copies, single source of truth     |
| Organization     | Good    | Clean file-per-concern layout                                  |
| Test coverage    | Partial | HTTP well-tested (10 tests); SSH untested                      |
| Security posture | Strong  | TLS 1.3 + PQ hybrid, Ed25519-only SSH, AEAD ciphers, ETM MACs |
