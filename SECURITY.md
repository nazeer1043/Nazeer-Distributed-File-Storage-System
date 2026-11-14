# NazeerDFS Security Policy & Threat Model

NazeerDFS is engineered with defense-in-depth principles across transport, storage at rest, identity, and application tiers.

---

## 1. Threat Model

NazeerDFS assumes the following threat environment:

| Threat Vector | Attack Scenario | NazeerDFS Mitigation |
| :--- | :--- | :--- |
| **Untrusted Network** | Adversary snooping or tampering with P2P cluster traffic | Mutual HMAC challenge-response peer authentication; length-prefixed binary framing; authenticated chunked encryption on all wire payloads. |
| **Storage Theft / Bitrot** | Adversary accesses local disk storage directly, or storage medium suffers corruption | All files are stored as AES-256-GCM authenticated ciphertext with counter-derived nonces. Data cannot be read without `ENCRYPTION_KEY`. Bitrot or tampering triggers decryption authentication failure. |
| **Rogue Peer Injection** | Attacker connects a malicious TCP socket into node ports (`:3000`, `:4000`) | Unauthenticated connections are rejected and disconnected immediately if they cannot complete the mutual HMAC-SHA256 handshake with `CLUSTER_SECRET`. |
| **Cross-User Data Access** | User Alice attempts to read, list, or delete User Bob's files | Strict server-side session ownership enforcement. Lookups are strictly CAS-key based; filename search across users is prohibited. Non-admins cannot inspect admin routes. |
| **Credential & Session Theft** | Password cracking or session hijacking | Passwords hashed with `bcrypt` (cost 12, min length 12). Sessions stored as SHA-256 hashes in MySQL with `HttpOnly`, `SameSite=Lax`, and `Secure` cookie flags. Session token rotated on login. |
| **Cross-Site Request Forgery** | Malicious web page attempts state-changing actions | Double-submit CSRF cookie token verification required on all state-changing HTTP requests (`POST`, `PUT`, `DELETE`). |
| **Denial of Service (DoS)** | Giant multipart upload buffer exhaustion or brute-force login attempts | Streaming uploads via `r.MultipartReader()` into temp files with `http.MaxBytesReader` limits (no whole-file RAM buffering). Rate limiting & 5-minute lockout after 5 consecutive failed logins. |

---

## 2. Cryptographic Specifications

- **Password Hashing**: `golang.org/x/crypto/bcrypt` with minimum work factor 12. Enforces minimum 12-character passwords.
- **Data at Rest Encryption**: AES-256-GCM in authenticated 64KB chunks.
  - Header: `NDFS` (4 bytes) + version `0x01` (1 byte) + Key ID (length-prefixed) + Base Nonce (12 bytes random from `crypto/rand`).
  - Derived Nonces: `baseNonce ^ chunkIndex`.
  - Additional Authenticated Data (AAD): Includes 64-bit chunk counter and EOF flag.
- **P2P Authentication**: HMAC-SHA256 mutual challenge-response with `CLUSTER_SECRET`.
- **Session Tokens**: Cryptographically secure 256-bit random tokens from `crypto/rand`, stored in MySQL as `SHA-256(token)`.

---

## 3. Key Rotation Procedure

NazeerDFS ciphertexts embed a versioned `KeyID` (e.g., `k1`, `k2`) in their header, enabling zero-downtime key rotation:

### Step 1: Generate New 32-Byte Key
Generate a secure base64-encoded 256-bit key:
```bash
openssl rand -base64 32
```

### Step 2: Configure Dual-Key Keyring
In the configuration / environment variables, deploy the new key as primary (`ENCRYPTION_KEY`) with `KEY_ID=k2`, while maintaining the previous key in `ENCRYPTION_KEY_FALLBACK` with `FALLBACK_KEY_ID=k1`.

### Step 3: Rolling Deployment
Perform a rolling restart of storage nodes and API instances:
1. All newly uploaded files will be encrypted using `k2`.
2. Existing files with `k1` headers will continue to be decrypted using the fallback key.

### Step 4: Background Re-encryption (Optional)
Run the migration script to read existing files, decrypt with `k1`, and rewrite with `k2`.

### Step 5: Decommission Old Key
Remove `ENCRYPTION_KEY_FALLBACK` once all files are updated to `k2`.

---

## 4. Reporting a Security Vulnerability

If you discover a security vulnerability in NazeerDFS:
1. Do **NOT** open a public GitHub issue.
2. Email full technical details and reproduction steps to the security team.
3. Allow up to 48 hours for an acknowledgment and coordinated disclosure schedule.
