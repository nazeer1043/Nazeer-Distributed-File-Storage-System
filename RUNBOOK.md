# NazeerDFS Operations Runbook

This runbook covers day-to-day operations, deployments, telemetry monitoring, disaster recovery, and incident troubleshooting for NazeerDFS clusters.

---

## 1. Quickstart & Deployment

### Option A: Docker Compose (Recommended for Production Stack)
1. Copy `.env.example` to `.env` and configure strong secrets:
   ```bash
   cp .env.example .env
   ```
2. Start the cluster and MySQL database:
   ```bash
   docker compose up -d --build
   ```
3. Check container status:
   ```bash
   docker compose ps
   ```
4. Access the web dashboard at `http://localhost:8080`.

### Option B: Dedicated Standalone Binaries
To run storage nodes and API gateways as independent processes:
```bash
# 1. Build binaries
go build -o bin/nazeerdfs-api ./cmd/api
go build -o bin/nazeerdfs-node ./cmd/node

# 2. Launch Bootstrap Storage Node 1
NODE_ADDR=:3000 STORAGE_ROOT=./data/node3000 CLUSTER_SECRET=your_secret ./bin/nazeerdfs-node &

# 3. Launch Storage Node 2 (peered to Node 1)
NODE_ADDR=:4000 STORAGE_ROOT=./data/node4000 BOOTSTRAP_NODES=localhost:3000 CLUSTER_SECRET=your_secret ./bin/nazeerdfs-node &

# 4. Launch HTTP API Gateway & Web UI
HTTP_PORT=8080 NODE_LISTEN_ADDRS=":3000,:4000" CLUSTER_SECRET=your_secret ./bin/nazeerdfs-api
```

### Option C: Deploying to Render
1. Connect your repository to Render as a **Web Service**.
2. Select **Docker** as the Runtime (uses the root `Dockerfile`).
3. Set the following environment variables in the Render dashboard:
   - `HTTP_PORT`: `8080` (or leave default, Render sets `PORT`)
   - `APP_ENV`: `production`
   - `ENCRYPTION_KEY`: A 32-byte base64 string
   - `CLUSTER_SECRET`: Strong secret for HMAC cluster handshakes
   - `ADMIN_BOOTSTRAP_PASSWORD`: At least 12 characters for initial admin bootstrap
   - `MYSQL_DSN`: Your hosted MySQL connection string (e.g. Render MySQL or Aiven)

---

## 2. Configuration Reference

| Environment Variable | Required in Prod? | Default | Description |
| :--- | :--- | :--- | :--- |
| `APP_ENV` | Optional | `development` | Setting to `production` enforces strict secret validation. |
| `HTTP_PORT` / `PORT` | Optional | `8080` | Port for the HTTP API and Web UI frontend. |
| `ENCRYPTION_KEY` | **Yes (Prod)** | (ephemeral dev key) | 32-byte base64 AES-256 key for data-at-rest encryption. |
| `CLUSTER_SECRET` | **Yes (Prod)** | (fallback dev key) | Shared secret for mutual HMAC-SHA256 peer authentication. |
| `ADMIN_BOOTSTRAP_PASSWORD` | **Yes (Prod)** | `admin12345678` | Password for the initial admin user bootstrapped on first run. |
| `NODE_LISTEN_ADDRS` | Optional | `:3000,:4000,:5000` | Comma-separated TCP listen addresses for P2P nodes. |
| `BOOTSTRAP_NODES` | Optional | `:3000` | Peer nodes to dial on startup. |
| `STORAGE_ROOTS` | Optional | `./3000_network,...` | Directories for node storage. |
| `MYSQL_DSN` | Optional | (uses DB_* vars) | Complete MySQL connection DSN with `parseTime=true`. |
| `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASS`, `DB_NAME` | Optional | `127.0.0.1`, `3306`, ... | Individual MySQL connection parameters. |
| `MAX_UPLOAD_SIZE` | Optional | `1073741824` (1GB) | Max allowed upload size in bytes. |

---

## 3. Monitoring & Telemetry

### Liveness Probe
- **Endpoint**: `GET /healthz`
- **Expected Status**: `200 OK`
- **Output**: `{"status":"alive", "timestamp":"..."}`

### Readiness Probe
- **Endpoint**: `GET /readyz`
- **Expected Status**: `200 OK` when healthy, `503 Service Unavailable` if degraded
- **Validates**:
  - At least one active storage node is responsive in the cluster
  - MySQL database connection is live and pingable

### Prometheus Metrics
- **Endpoint**: `GET /metrics`
- **Scrape Frequency**: 15s - 30s
- **Key Metrics**:
  - `nazeerdfs_uptime_seconds`: Process uptime.
  - `nazeerdfs_active_nodes`: Number of live, healthy storage nodes.
  - `nazeerdfs_total_nodes`: Total configured storage nodes.
  - `nazeerdfs_stored_files`: Total file objects stored.
  - `nazeerdfs_stored_bytes`: Total byte volume in cluster.
  - `nazeerdfs_gdrive_connected`: `1` if Google Drive is authorized, `0` if disabled.

---

## 4. Disaster Recovery & Backups

### MySQL Database Backup
Export the primary metadata, user accounts, and session tables:
```bash
mysqldump -u root -p nazeerdfs > nazeerdfs_backup_$(date +%F).sql
```

### Restoring MySQL Metadata
```bash
mysql -u root -p nazeerdfs < nazeerdfs_backup_2026-11-14.sql
```

### Google Drive Cold Tier Recovery
If all local storage node disks are corrupted or lost:
1. Ensure `credentials.json` and `token.json` are present.
2. The server will automatically query Google Drive upon download requests when files are not found locally or in peer networks, restoring the ciphertext stream.

---

## 5. Troubleshooting & Incident Response

### Problem 1: `503 Service Unavailable` on `/readyz` or `/api/upload`
- **Cause**: No storage nodes are running or healthy, or MySQL ping is failing.
- **Action**: Check storage node logs (`docker compose logs nazeerdfs-cluster`). Verify storage nodes are listening on their respective ports (`:3000`, `:4000`, `:5000`).

### Problem 2: `p2p authentication failed: unauthorized cluster secret`
- **Cause**: Nodes have mismatched `CLUSTER_SECRET` environment variables.
- **Action**: Verify all cluster nodes share the exact same `CLUSTER_SECRET` string.

### Problem 3: File Checksum Mismatch on Download
- **Cause**: Disk corruption or manual modification of stored ciphertext.
- **Action**: NazeerDFS automatically detects checksum mismatch upon decryption and transparently fetches an uncorrupted replica from peer nodes or Google Drive. Check audit logs to identify which node suffered corruption.
