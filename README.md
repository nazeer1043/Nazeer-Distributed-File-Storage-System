# NazeerDFS — Distributed File Storage System

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

NazeerDFS is a decentralized, content-addressed peer-to-peer (P2P) file storage system implemented in Go. It provides chunked local data distribution across cluster nodes, authenticated AES-256-GCM encryption, optional Google Drive cloud backup fallback, MySQL metadata persistence, and an enterprise web management console.

---

## Architecture Overview

```
                        +---------------------------+
                        |      Web UI / Client      |
                        +-------------+-------------+
                                      | HTTP REST
                                      v
                        +---------------------------+
                        |   NazeerDFS HTTP / API    |
                        +-------------+-------------+
                                      |
            +-------------------------+-------------------------+
            |                         |                         |
            v                         v                         v
+-----------------------+ +-----------------------+ +-----------------------+
|  Storage Node (:3000) | |  Storage Node (:4000) | |  Storage Node (:5000) |
|   Local CAS Store     | |   Local CAS Store     | |   Local CAS Store     |
+-----------+-----------+ +-----------+-----------+ +-----------+-----------+
            ^                         ^                         ^
            +================== P2P Mesh =======================+
                                      |
                                      v (Fallback)
                        +---------------------------+
                        |  Google Drive Cloud Tier  |
                        +---------------------------+
```

- **Content-Addressable Storage (CAS):** Files are chunked and placed into directory trees determined by SHA-256 digests (`CASPathTransformFunc`), preventing single-folder inode saturation.
- **P2P Transport Mesh:** Custom TCP transport layer using framed binary protocols for node discovery, synchronization, and streaming replication.
- **Data Protection:** Files are encrypted at rest using authenticated chunked encryption with persistent 32-byte keys.
- **Dual-Tier Resilience:** If a requested file chunk is missing across all local peer nodes, NazeerDFS falls back to an encrypted Google Drive vault.
- **MySQL Metadata & Auth:** Stores user accounts, sessions, access control rules, file checksums, and audit logs.

---

## Quickstart

### Prerequisites
- Go 1.22 or later
- MySQL 8.0+ (optional; an in-memory fallback runs automatically for local development)
- Git

### 1. Clone & Setup
```bash
git clone https://github.com/nazeer1043/Nazeer-Distributed-File-Storage-System.git
cd Nazeer-Distributed-File-Storage-System
cp .env.example .env
```

### 2. Configure Environment
Edit `.env` to supply an encryption key and cluster secrets:
```bash
# Generate a 32-byte Base64 key:
# openssl rand -base64 32
ENCRYPTION_KEY=<your-base64-key>
CLUSTER_SECRET=<your-cluster-secret>
```

### 3. Run Locally
```bash
go run cmd/main.go
```
The HTTP web console and API will start at `http://localhost:8080`.

### 4. Run with Docker Compose
```bash
docker compose up -d --build
```

---

## Configuration Reference

Configuration is managed entirely through environment variables. See [`.env.example`](.env.example) for defaults.

| Variable | Description | Default |
| :--- | :--- | :--- |
| `APP_ENV` | Application environment (`development`, `production`) | `development` |
| `PORT` / `HTTP_PORT` | HTTP server port | `8080` |
| `NODE_LISTEN_ADDRS` | Comma-separated TCP listen addresses for P2P nodes | `:3000,:4000,:5000` |
| `STORAGE_ROOTS` | Comma-separated paths for local storage directories | `3000_network,4000_network,5000_network` |
| `BOOTSTRAP_NODES` | P2P seed nodes for discovery | `:3000,:4000` |
| `ENCRYPTION_KEY` | Base64-encoded 32-byte master encryption key | *(required in production)* |
| `CLUSTER_SECRET` | Secret string for node-to-node mutual authentication | *(required in production)* |
| `ADMIN_BOOTSTRAP_PASSWORD` | Initial admin account password (min 12 chars) | — |
| `MYSQL_DSN` | MySQL database connection string | `root:@tcp(127.0.0.1:3306)/nazeerdfs?parseTime=true` |
| `CREDENTIALS_JSON` | Google Cloud service account or OAuth credentials JSON | — |
| `TOKEN_JSON` | Google Drive OAuth refresh token JSON | — |

---

## Testing & Verification

Run the full test suite with data race detection:
```bash
go test -race -v ./...
```

Run linter and static analysis:
```bash
go vet ./...
```

---

## Documentation

- [**System Architecture**](ARCHITECTURE.md) — Comprehensive technical design, CAS specification, P2P binary framing wire format, and quorum replication.
- [**REST API Reference**](API.md) — Detailed endpoint documentation, double-submit CSRF, request/response models, and curl examples.
- [**User & Administrator Guide**](USER_GUIDE.md) — Web console walkthrough, file management, upload center, role administration, and Google Drive setup.
- [**Operations Runbook**](RUNBOOK.md) — Production setup, Docker Compose, Render deployment, monitoring, telemetry, and disaster recovery.
- [**Developer & Contributor Guide**](DEVELOPER_GUIDE.md) — Code organization, multi-process testing, fuzzing, and security audits.
- [**Security Policy & Threat Model**](SECURITY.md) — Threat model, cryptographic primitives, authentication, and zero-downtime key rotation guide.
- [**Status & Limitations**](STATUS.md) — Verified production readiness checklist and documented architectural boundaries.

---

## Status & Limitations

See [`STATUS.md`](STATUS.md) for current engineering limits and design trade-offs.

---

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
