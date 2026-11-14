# NazeerDFS Developer & Contributor Guide

This guide details code organization, development workflows, testing suites, fuzzing, and security verification procedures for contributors working on the NazeerDFS codebase.

---

## 1. Codebase Structure

```
distributed-file-storage/
├── cmd/
│   ├── main.go          # All-in-one local development entrypoint (launches nodes + API)
│   ├── api/             # Dedicated HTTP REST API gateway & Web UI server
│   └── node/            # Headless P2P storage node process
├── pkg/
│   └── api/             # HTTP route handlers, middlewares (CSRF, auth, logging), node router
├── fileserver/          # Core storage engine, metadata store, and async Google Drive queue
├── p2p/                 # Length-prefixed framed binary wire protocol & HMAC challenge handshake
├── crypto/              # Versioned authenticated chunked encryption (AES-256-GCM)
├── store/               # CAS disk path transformation and Google Drive client implementation
├── db/                  # MySQL connection pool wrapper and in-memory test fallback
├── migrations/          # Versioned SQL migrations (golang-migrate embedded FS)
├── web/                 # Static web console assets (HTML, CSS, JS, Lucide icons)
└── .github/workflows/   # Continuous integration pipeline definition
```

---

## 2. Setting Up the Local Environment

### Prerequisites
- **Go 1.22+** (tested through Go 1.24)
- **Git**
- **Docker & Docker Compose** (optional, for local containerized development)
- **MySQL 8.0+** (optional; memory fallback operates automatically)

### Initial Setup
```bash
git clone https://github.com/nazeer1043/Nazeer-Distributed-File-Storage-System.git
cd Nazeer-Distributed-File-Storage-System
cp .env.example .env
go mod download
```

---

## 3. Running & Debugging

### Option 1: All-in-One Development Process
Runs 3 in-process P2P storage nodes (`:3000`, `:4000`, `:5000`) and the HTTP Web console (`:8080`) simultaneously:
```bash
go run cmd/main.go
```

### Option 2: Running Standalone Components
For multi-process topology testing:
```bash
# Terminal 1: Storage Node 1
NODE_ADDR=:3000 STORAGE_ROOT=./data/n1 CLUSTER_SECRET=devsecret go run cmd/node/main.go

# Terminal 2: Storage Node 2 (Peered to Node 1)
NODE_ADDR=:4000 STORAGE_ROOT=./data/n2 BOOTSTRAP_NODES=localhost:3000 CLUSTER_SECRET=devsecret go run cmd/node/main.go

# Terminal 3: API Gateway
NODE_LISTEN_ADDRS=":3000,:4000" CLUSTER_SECRET=devsecret HTTP_PORT=8080 go run cmd/api/main.go
```

---

## 4. Test Suites & Verification

### 4.1 Unit & Integration Tests (With Race Detector)
Every module must pass tests with race detection enabled:
```bash
go test -race -v ./...
```

### 4.2 Wire Protocol Fuzz Testing
The binary framed decoder includes a continuous fuzz testing harness to verify resilience against malicious or truncated network packets:
```bash
go test -fuzz=FuzzFramedDecoder -fuzztime=30s ./p2p
```

### 4.3 Multi-Node Resiliency Test
The integration test `TestMultiNode_StoreNodeA_KillA_ReadNodeB_Integrity` verifies that if Node A accepts a write and is immediately terminated, Node B satisfies download requests and confirms checksum integrity.
```bash
go test -v -run TestMultiNode ./fileserver
```

---

## 5. Security & Quality Tooling

Before opening a pull request or pushing commits, run the automated verification toolchain:

### Go Vet
```bash
go vet ./...
```

### Secret Leak Detection (Gitleaks)
Ensures zero API keys, passwords, or tokens exist in git history or working tree:
```bash
gitleaks detect --source . -v
```

### Security Audit (Gosec)
Scans for AST security flaws (e.g. integer overflows, directory traversal, timing attacks):
```bash
gosec -quiet -exclude=G104 ./...
```

### Vulnerability Database Scan (Govulncheck)
Checks Go dependencies against the official Go vulnerability database:
```bash
govulncheck ./...
```

---

## 6. Database Migrations

Database schema updates must use versioned SQL files under `migrations/`:
1. Add new migration pairs:
   - `migrations/00000X_description.up.sql`
   - `migrations/00000X_description.down.sql`
2. Migrations are automatically compiled into the binary via `//go:embed *.sql` and executed upon startup in `migrations.RunMigrations(db)`.
