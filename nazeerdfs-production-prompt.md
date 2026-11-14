# NazeerDFS: Production-Readiness Prompt

Paste everything below the line into a coding agent with repo access (e.g. Claude Code).

**Before you run it:** rotate the Google OAuth client secret and refresh token yourself. Do not paste credentials into the prompt.

---

You are working in a Go repo (module github.com/yigithankarabulut/distributed-file-storage, "NazeerDFS"): a P2P content-addressed file store with a web UI, MySQL persistence and a Google Drive fallback. I want it production-ready. Work in the phases below, in order. Commit once per phase. Run `go build ./...`, `go vet ./...` and `go test -race ./...` after each phase and fix failures before moving on. Never print or commit secrets. Do not claim anything is done without showing test or command output as evidence. If something can't be fully solved, say so and document it instead of papering over it.

## Phase 0: Repo hygiene and secrets
- Untrack data and binaries: `git rm -r --cached` the `*_network/` dirs, `main.exe`, and any credentials/token files. Confirm .gitignore covers them.
- Move all config to environment variables (listen addrs, MySQL DSN, Google credentials via CREDENTIALS_JSON/TOKEN_JSON, encryption key, cluster secret, admin bootstrap password). Validate at startup and fail fast with clear errors. Add `.env.example` with placeholders only.
- Delete PROJECT_READINESS_REPORT.md. Its scores are unverified and contradict the code. Replace it with a short STATUS.md that is honest about limits.
- Rewrite README for this project (not the tutorial). Add a LICENSE file, since the README references MIT.

## Phase 1: Encryption
- The key is currently regenerated on every start (cmd/main.go), which makes replicated and Drive data undecryptable after a restart. Load a persistent 32-byte key from env (base64) or a mounted file. Refuse to start in production without it. Add a key ID in the ciphertext header so rotation is possible later.
- Replace AES-CTR streaming with authenticated chunked encryption (e.g. per-chunk AES-GCM with counter-derived nonces, or XChaCha20-Poly1305), with a versioned header. Tampered, truncated or reordered chunks must fail.
- Encrypt before the local write, so data at rest is never plaintext.
- Fix copyStream in crypto/crypto.go: it returns the wrong error variable on write failure, and its byte count is wrong.
- Tests: round trip, tamper detection, truncation, wrong key, empty and very large files.

## Phase 2: Authentication
- Replace unsalted SHA-256 with argon2id or bcrypt. Minimum password length 12.
- Remove the hardcoded admin/admin123 hash and the "admin quick login". Bootstrap the first admin from env on first run only.
- New registrations get role "user", never "Cluster Admin". Add real roles (user, admin).
- Store sessions in MySQL (hashed tokens, expiry), not process memory, so restarts and multiple instances work. Cookie flags: HttpOnly, Secure, SameSite=Lax or Strict. Rotate the session on login and invalidate on logout.
- Add CSRF protection for state-changing routes, and rate limiting plus temporary lockout on login/register.
- Add security headers (CSP, X-Content-Type-Options, frame-ancestors, HSTS behind TLS).

## Phase 3: Authorization
- Take `owner` from the session, never from a form field.
- Enforce ownership (or admin) on list, download and delete. Look files up by key only, never by filename across all users.
- Restrict /api/logs, /api/nodes, /api/cluster, /api/settings and /api/users to admins. /api/users must never return password hashes.
- Build Content-Disposition with mime.FormatMediaType and sanitize filenames. Set nosniff.
- Tests: user A cannot read, list or delete user B's files; non-admins get 403 on admin routes.

## Phase 4: Uploads and CAS
- Stream uploads with r.MultipartReader into a temp file while hashing. No ParseMultipartForm(1GB), no io.ReadAll, and no whole-file buffers in fileserver.Store/StoreWithMeta or the GDrive path. Enforce a configurable max size with http.MaxBytesReader.
- Make the key genuinely content-derived: full SHA-256 of the plaintext content, namespaced per owner. Filename lives in metadata only. Deduplicate identical content per owner and use reference counting so delete is safe.
- Make MySQL the source of truth for metadata. Keep metadata.json only as an optional dev fallback.

## Phase 5: P2P protocol
- Replace the no-op handshake with mutual authentication (TLS with a cluster CA, or HMAC challenge-response using a cluster secret). Unauthenticated peers must be rejected, and delete messages must only be accepted from authenticated peers.
- Remove every time.Sleep used for synchronization. Use a length-prefixed framed protocol (message type, stream ID, length) so control messages and data streams can't race.
- Add read/write deadlines, bounded message sizes, peer reconnect with backoff, and cleanup on disconnect.
- Add a configurable replication factor with acknowledgements and a write quorum. Verify the checksum on read and try the next peer or Drive on mismatch.
- Fuzz-test the decoder. Add a multi-node integration test: store on node A, kill A, read from B, then check integrity.

## Phase 6: Topology and deployment
- Split into cmd/node (storage node, own process) and cmd/api (HTTP). Nodes are configured via env, not hardcoded in main. Stop hardwiring `Servers[0]`: route by round-robin or consistent hashing, with health checks.
- Make Google Drive backup an async queue with retries and backoff, with status recorded in metadata.
- Vercel serverless can't host long-lived TCP P2P nodes or in-memory state. Either remove vercel.json and api/index.go or restrict them to a documented, limited role. Target Docker and Render.
- Remove the Windows-only killProcessOnPort from production paths.

## Phase 7: Operations
- Structured logging (log/slog) with request IDs, and no secrets or tokens in logs.
- Add /healthz and /readyz, Prometheus metrics, graceful shutdown for HTTP and nodes, and http.Server timeouts including ReadHeaderTimeout.
- Move schema.sql to versioned migrations (golang-migrate). Configure DB pool settings.
- Dockerfile: multi-stage, non-root, minimal image. docker-compose with healthchecks.
- GitHub Actions CI: go vet, golangci-lint (use the existing .golangci.yml), go test -race with coverage, govulncheck, gosec.

## Phase 8: Docs
- ARCHITECTURE.md describing the real design, plus SECURITY.md with threat model and key rotation steps, and RUNBOOK.md. Explicitly list what is NOT provided, such as automatic rebalancing, repair after node loss beyond replication, and multi-region.

## Definition of done
Report a checklist with evidence for each item:
- Restart the stack and a previously stored file is still downloadable.
- Cross-user access tests pass.
- Unauthenticated peer connection is rejected.
- A 2GB upload does not grow memory beyond a stated bound.
- `go test -race ./...`, govulncheck and gosec are clean, or each exception is justified.
- No secrets in git history or the working tree (run a scanner such as gitleaks).
- README, STATUS.md and ARCHITECTURE.md match the code.

At the end, list the remaining limitations honestly.
