# NazeerDFS Architecture & Status

## Overview
NazeerDFS is a peer-to-peer (P2P), content-addressed distributed file storage system written in Go. It provides chunked local file storage across interconnected nodes, dual-tier Google Drive cloud fallback, MySQL metadata persistence, and a web dashboard.

---

## Current Status & Verification

This document provides a realistic, verified technical status of the codebase across all core subsystems.

### Core Capabilities
- **Content-Addressed Storage (CAS):** Files are chunked and placed into directory trees transformed from SHA-256 digests (`CASPathTransformFunc`), preventing filesystem directory saturation.
- **HTTP REST & Dashboard:** Go `net/http` server serving a responsive web interface and RESTful endpoints for file upload, download, metadata inspection, and cluster monitoring.
- **Dual-Tier Storage Fallback:** When a requested file is absent from the local storage root and peer network, nodes can query a Google Drive backup vault if credentials are provided.
- **MySQL Metadata Persistence:** Stores file metadata, user accounts, and node states in MySQL with an automatic in-memory fallback for local development or disconnected testing.

---

## Known Limits & Architectural Constraints

1. **P2P Transport & Framing:**
   - Peer synchronization historically used basic TCP streams and `time.Sleep` pauses rather than framed binary protocols. This is undergoing overhaul to length-prefixed binary framing with mutual authentication.
2. **Encryption at Rest:**
   - The original implementation derived ephemeral keys per process run. Key management is being shifted to persistent 32-byte keys loaded strictly via environment variables (`ENCRYPTION_KEY`) or mounted secret files with versioned ciphertext headers.
3. **Upload Buffering & Streaming:**
   - Large file uploads previously buffered into memory. Streaming via `r.MultipartReader()` directly to temporary disk buffers is required to handle multi-gigabyte uploads within bounded memory (<100MB RAM).
4. **Topology & Consensus:**
   - The cluster uses broadcast replication with best-effort peer synchronization. It does not implement Paxos/Raft consensus or automated data rebalancing when a node is permanently decommissioned.
5. **Deployment Model:**
   - Serverless platforms (e.g. Vercel) cannot host persistent TCP listening sockets or long-lived P2P peer meshes. NazeerDFS is designed for containerized environments (Docker, Render, Kubernetes, or bare VMs) with persistent disk mounts.
