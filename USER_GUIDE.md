# NazeerDFS User & Administrator Guide

Welcome to the **NazeerDFS Enterprise Console Guide**. This walkthrough explains how to utilize both standard user features and administrative capabilities within the NazeerDFS web management interface.

---

## 1. Getting Started

### Accessing the Web Application
Open your browser and navigate to:
- **Local Dev**: `http://localhost:8080`
- **Render Cloud**: `https://<your-service>.onrender.com`

---

## 2. Authentication & Account Management

### Registering a New Account
1. Click **"Create Account"** on the login page (`register.html`).
2. Provide your Full Name, Username, Organization, and a strong Password.
3. Passwords must be at least **12 characters** in length.
4. Newly registered users are assigned the standard **User** role.

### Logging In
1. Enter your username and password.
2. Upon successful authentication, a secure, `HttpOnly` encrypted session cookie is established and you are redirected to the **Dashboard**.

### Profile & Password Management
1. In the top navigation bar or sidebar, click **Settings** → **Account**.
2. **Update Personal Information**: Edit your full name or organization name, then click **Save Profile**.
3. **Change Password**: Provide your current password, enter a new password (min 12 characters), confirm, and click **Update Password**.

---

## 3. Web Console Navigation

### 3.1 Dashboard (`dashboard.html`)
The central operations overview:
- **Cluster Capacity**: Total storage capacity, used space, and percentage utilization.
- **Active Node Health**: Live count of online storage nodes vs. total configured nodes.
- **Storage Tier Distribution**: Visual breakdown between local node storage and Google Drive cloud storage.
- **Recent Activity**: Audit feed showing recent uploads, downloads, and peer synchronization events.

### 3.2 My Files (`files.html`)
Manage and inspect your stored files:
- **Search & Filter**: Search files by filename or key in real time.
- **Content-Addressable Details**: View each file's computed CAS Key, plaintext SHA-256 Checksum, size, and replication status.
- **Download**: Click the download icon to stream the decrypted file to your machine. NazeerDFS streams the download with full checksum integrity verification.
- **Delete**: Click the trash icon to remove your file reference. Reference counting ensures underlying data blocks are only pruned once no other user references the same content.

### 3.3 Upload Center (`upload.html`)
High-performance streaming upload interface:
- **Drag-and-Drop**: Drag any file from your desktop into the upload target area.
- **Real-Time Progress**: Monitors network transfer speed, elapsed time, and bytes sent.
- **Client-Side CAS Pipeline**:
  1. Streams directly into the storage engine.
  2. Encrypts data at rest using AES-256-GCM.
  3. Propagates replicas across healthy storage nodes according to the cluster replication factor.
  4. Enqueues a background cold-tier backup to Google Drive.

### 3.4 Storage Nodes (`nodes.html`) *(Admin View)*
Real-time hardware and disk monitoring:
- Inspect active node TCP addresses (e.g. `:3000`, `:4000`, `:5000`).
- View per-node disk usage, free space, and active streaming channels.
- Monitor node heartbeats and health status.

### 3.5 Cluster Monitor (`cluster.html`) *(Admin View)*
Topological mesh visualization:
- Real-time peer connection graph.
- Quorum verification status (`(N/2) + 1` quorum).
- Replication latency and inter-node synchronization rates.

### 3.6 Settings & Users Directory (`settings.html`)
- **Theme Switcher**: Seamlessly toggle between **Dark Mode** and **Light Mode** using the sun/moon toggle in the navbar.
- **Registered Cluster Accounts** *(Admin Only)*: Review all registered platform users, their roles, organizations, and creation dates. Password hashes are never exposed.

---

## 4. Google Drive 400GB Cloud Backup Tier

NazeerDFS includes an automated secondary cloud storage tier that backs up encrypted CAS chunks to a designated Google Drive folder.

### Enabling Google Drive Integration:
1. Obtain an `OAuth 2.0 Client ID` (Web application) from the [Google Cloud Console](https://console.cloud.google.com/).
2. Place the downloaded JSON file as `credentials.json` in the root of the project directory (or supply via `CREDENTIALS_JSON` environment variable).
3. On first startup, the server displays an authorization URL in the terminal logs:
   ```text
   =====================================================================
   [GDrive] Action Required: Click URL to authorize your 400GB Google Drive:
   https://accounts.google.com/o/oauth2/auth?...
   =====================================================================
   ```
4. Authorize access with your Google account. A `token.json` file is generated automatically.
5. All future file uploads will asynchronously sync encrypted replicas to your Google Drive folder. If all local disks are ever destroyed, files can be completely restored from this cloud backup!
