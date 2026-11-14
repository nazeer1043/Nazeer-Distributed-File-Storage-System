# NazeerDFS REST API Specification & Reference

Welcome to the **NazeerDFS REST API Reference**. This document provides detailed technical specifications for all HTTP endpoints exposed by the NazeerDFS API Gateway, including authentication mechanisms, double-submit CSRF protection, file streaming protocols, and error representations.

---

## 1. API Architecture & Standards

- **Base URL (Local)**: `http://localhost:8080`
- **Base URL (Render/Production)**: `https://<your-service>.onrender.com`
- **Wire Encoding**: All JSON requests and responses use UTF-8 `application/json`.
- **File Payloads**: File uploads are processed as `multipart/form-data` streams; file downloads are served as binary byte streams (`application/octet-stream`).
- **Telemetry**: Every incoming HTTP request is stamped with an `X-Request-ID` header for distributed tracing.

---

## 2. Authentication & Security Middleware

NazeerDFS implements stateful session authentication backed by MySQL database persistence (with automatic local dev fallback):

### 2.1 Cookies & Headers
- **Session Cookie (`nazeerdfs_session`)**: 32-byte cryptographically secure hex string. Hashed using SHA-256 before storage in the database. Cookies are marked `HttpOnly; SameSite=Lax` (and `Secure` in production).
- **CSRF Token Header (`X-CSRF-Token`)**: Double-submit CSRF cookie (`nazeerdfs_csrf`). All state-changing HTTP requests (`POST`, `PUT`, `DELETE`, `PATCH`) must include the `X-CSRF-Token` header matching the cookie value.

### 2.2 Security Headers Enforced on Every Response
```http
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Content-Security-Policy: default-src 'self'; script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com https://unpkg.com; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; img-src 'self' data: https://api.dicebear.com;
Strict-Transport-Security: max-age=31536000; includeSubDomains (production only)
```

---

## 3. Authentication Endpoints

### `POST /api/register`
Creates a new standard user account. New self-registrations are strictly provisioned with role `user`.

#### Request
```http
POST /api/register HTTP/1.1
Content-Type: application/json
X-CSRF-Token: <csrf-token>

{
  "username": "alice",
  "password": "CorrectHorseBatteryStaple123!",
  "fullName": "Alice Smith",
  "organization": "Security Operations"
}
```
*Note: Passwords must be at least 12 characters in length.*

#### Response (201 Created)
```json
{
  "success": true,
  "message": "Account created successfully. You can now log in."
}
```

---

### `POST /api/login`
Authenticates user credentials using `bcrypt` and creates a persistent database session.

#### Request
```http
POST /api/login HTTP/1.1
Content-Type: application/json

{
  "username": "alice",
  "password": "CorrectHorseBatteryStaple123!"
}
```

#### Response (200 OK)
```json
{
  "success": true,
  "user": {
    "username": "alice",
    "fullName": "Alice Smith",
    "role": "user",
    "organization": "Security Operations"
  }
}
```
*Sets `Set-Cookie: nazeerdfs_session=...; HttpOnly; SameSite=Lax` and `Set-Cookie: nazeerdfs_csrf=...`*

---

### `POST /api/logout`
Invalidates and deletes the active session from MySQL and clears authentication cookies.

#### Request
```http
POST /api/logout HTTP/1.1
X-CSRF-Token: <csrf-token>
```

#### Response (200 OK)
```json
{
  "success": true,
  "message": "Logged out successfully"
}
```

---

### `GET /api/session`
Checks the validity of the current caller's session token.

#### Response (200 OK)
```json
{
  "authenticated": true,
  "user": {
    "username": "alice",
    "fullName": "Alice Smith",
    "role": "user",
    "organization": "Security Operations"
  }
}
```

---

### `GET /api/profile`
Retrieves detailed profile metadata for the currently authenticated user.

#### Response (200 OK)
```json
{
  "username": "alice",
  "fullName": "Alice Smith",
  "role": "user",
  "organization": "Security Operations",
  "createdAt": "2026-11-14T19:30:00Z"
}
```

---

### `POST /api/profile`
Updates personal user information or changes the account password.

#### Request: Updating Details
```json
{
  "action": "update_details",
  "fullName": "Alice Johnson",
  "organization": "Engineering"
}
```

#### Request: Changing Password
```json
{
  "action": "change_password",
  "currentPassword": "CorrectHorseBatteryStaple123!",
  "newPassword": "NewSuperStrongPassword2026!"
}
```

---

## 4. File Storage & Content-Addressed Storage (CAS)

### `POST /api/upload`
Streams file upload via `r.MultipartReader()` directly to temporary storage while calculating plaintext SHA-256 and encrypting at rest.

- **Authentication**: Required (`user` or `admin`).
- **Body**: `multipart/form-data` with part field name `file`.
- **Memory Consumption**: Strictly bounded ($O(1)$ stream buffer). Whole files are never loaded into process memory.

#### Example `curl` Upload:
```bash
curl -X POST https://<your-host>/api/upload \
  -H "X-CSRF-Token: $CSRF_TOKEN" \
  -b "nazeerdfs_session=$SESSION_COOKIE" \
  -F "file=@presentation.pdf"
```

#### Response (200 OK)
```json
{
  "success": true,
  "key": "c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f2",
  "filename": "presentation.pdf",
  "size": 4194304,
  "checksum": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
  "replicatedNodes": [":3000", ":4000"],
  "gdriveStatus": "pending"
}
```

---

### `GET /api/download?key=<CAS_KEY>`
Streams and decrypts a content-addressed file object.

- **Authentication**: Required.
- **Access Control**: Users may only download files they own. Administrators may download any file.
- **Failover**: If a local storage node reports a checksum mismatch or disk failure, NazeerDFS automatically retrieves the ciphertext from peer nodes or the Google Drive cloud tier.

#### Response Headers:
```http
HTTP/1.1 200 OK
Content-Type: application/pdf
Content-Disposition: attachment; filename="presentation.pdf"
X-Content-Type-Options: nosniff
```

---

### `DELETE /api/delete?key=<CAS_KEY>`
Decrements the reference counter for a content-addressed file. When the reference count reaches zero across all users, the underlying physical ciphertext chunks are deleted from all cluster nodes and Google Drive.

#### Response (200 OK)
```json
{
  "success": true,
  "message": "File reference successfully removed"
}
```

---

### `GET /api/files`
Lists all files owned by the caller (or all cluster files if called by an administrator).

#### Query Parameters:
- `search` (optional): Filter by filename.
- `limit` (optional): Number of records (default: 50).
- `offset` (optional): Offset for pagination.

#### Response (200 OK)
```json
{
  "files": [
    {
      "key": "c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f2",
      "filename": "presentation.pdf",
      "size": 4194304,
      "sizeFormatted": "4.00 MB",
      "owner": "alice",
      "contentType": "application/pdf",
      "checksum": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
      "replicatedNodes": [":3000", ":4000"],
      "gdriveStatus": "synced",
      "createdAt": "2026-11-14T21:40:00Z"
    }
  ],
  "total": 1
}
```

---

## 5. Cluster & Administrative APIs

*Note: All endpoints in this section require `admin` role authorization. Non-admins receive `403 Forbidden`.*

### `GET /api/nodes`
Returns real-time health, latency, and disk metrics for all configured storage nodes.

#### Response (200 OK)
```json
{
  "nodes": [
    {
      "address": ":3000",
      "status": "online",
      "healthy": true,
      "diskUsedBytes": 146800640,
      "diskFreeBytes": 429496729600,
      "activeStreams": 0,
      "lastHeartbeat": "2026-11-14T22:20:00Z"
    },
    {
      "address": ":4000",
      "status": "online",
      "healthy": true,
      "diskUsedBytes": 146800640,
      "diskFreeBytes": 429496729600,
      "activeStreams": 0,
      "lastHeartbeat": "2026-11-14T22:20:00Z"
    }
  ]
}
```

---

### `GET /api/cluster`
Summarizes cluster topology, replication status, and write quorum health.

---

### `GET /api/users`
Lists all registered platform accounts. **Password hashes are never returned.**

#### Response (200 OK)
```json
{
  "users": [
    {
      "id": 1,
      "username": "admin",
      "fullName": "Cluster Administrator",
      "role": "admin",
      "organization": "Platform Ops",
      "createdAt": "2026-11-14T18:00:00Z"
    },
    {
      "id": 2,
      "username": "alice",
      "fullName": "Alice Johnson",
      "role": "user",
      "organization": "Engineering",
      "createdAt": "2026-11-14T19:30:00Z"
    }
  ]
}
```

---

### `GET /api/logs`
Returns the recent structured audit log stream with sensitive parameters redacted.

---

## 6. Observability & Health Probes

### `GET /healthz`
Kubernetes / Render liveness probe. Returns HTTP `200 OK` as long as the process is alive.

```json
{
  "status": "alive",
  "timestamp": "2026-11-14T22:24:00Z"
}
```

---

### `GET /readyz`
Readiness probe. Validates that:
1. At least one storage node is responsive and reachable.
2. The MySQL database connection pool is responsive (if configured).

- **Healthy Status**: `200 OK`
- **Degraded Status**: `503 Service Unavailable`

---

### `GET /metrics`
Prometheus exposition format for telemetry scrapers (Datadog, Prometheus, Grafana Agent).

```text
# HELP nazeerdfs_uptime_seconds Process uptime in seconds
# TYPE nazeerdfs_uptime_seconds gauge
nazeerdfs_uptime_seconds 1240.50

# HELP nazeerdfs_active_nodes Number of healthy connected storage nodes
# TYPE nazeerdfs_active_nodes gauge
nazeerdfs_active_nodes 3

# HELP nazeerdfs_total_nodes Total configured cluster nodes
# TYPE nazeerdfs_total_nodes gauge
nazeerdfs_total_nodes 3

# HELP nazeerdfs_stored_files Total number of files indexed
# TYPE nazeerdfs_stored_files gauge
nazeerdfs_stored_files 14

# HELP nazeerdfs_stored_bytes Total volume of data stored
# TYPE nazeerdfs_stored_bytes gauge
nazeerdfs_stored_bytes 58720256
```
