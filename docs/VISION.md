# oelala-storage: Architecture Vision

> **Last Updated**: 2026-01-15
> **Status**: CANONICAL - This is the authoritative document for oelala-storage architecture

---

## 🎯 Core Concept

**oelala-storage is a client/server distributed storage system**, where:

- **Clients (Nodes)** = Storage providers that donate disk space
- **Server (Coordinator)** = Orchestrates nodes, serves as CDN entry point
- **Backend Integration** = oelala-backend controls access, retention, visibility

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              ARCHITECTURE                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│                        ┌──────────────────────┐                             │
│                        │   oelala-backend     │                             │
│                        │   (Python/FastAPI)   │                             │
│                        │                      │                             │
│                        │ • User auth          │                             │
│                        │ • Access control     │                             │
│                        │ • Retention policies │                             │
│                        │ • Tier management    │                             │
│                        │ • Who sees what      │                             │
│                        └──────────┬───────────┘                             │
│                                   │                                          │
│                                   │ API calls                                │
│                                   ▼                                          │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │                    COORDINATOR (Server Mode)                            │ │
│  │                    oelala-storage --mode=coordinator                    │ │
│  │                                                                         │ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                  │ │
│  │  │ Node         │  │ File         │  │ CDN          │                  │ │
│  │  │ Registry     │  │ Placement    │  │ Entry Point  │                  │ │
│  │  │              │  │ Engine       │  │              │                  │ │
│  │  │ • Health     │  │              │  │ • Routes to  │                  │ │
│  │  │ • Capacity   │  │ • Which node │  │   correct    │                  │ │
│  │  │ • Status     │  │   stores what│  │   node       │                  │ │
│  │  └──────────────┘  └──────────────┘  └──────────────┘                  │ │
│  │                                                                         │ │
│  │  Database (BadgerDB / PostgreSQL):                                      │ │
│  │  ┌─────────────────────────────────────────────────────────────────┐   │ │
│  │  │ • Blob references (hash → node locations)                       │   │ │
│  │  │ • File metadata (user → files, expiration, etc.)               │   │ │
│  │  │ • Node registry (capacity, health, region)                      │   │ │
│  │  │ • Deduplication tracking (refcount per blob)                    │   │ │
│  │  └─────────────────────────────────────────────────────────────────┘   │ │
│  └────────────────────────────────────────────────────────────────────────┘ │
│                                   │                                          │
│                   ┌───────────────┼───────────────┐                         │
│                   │ gRPC          │               │ gRPC                    │
│                   ▼               ▼               ▼                         │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐          │
│  │   STORAGE NODE   │  │   STORAGE NODE   │  │   STORAGE NODE   │          │
│  │   (Linux)        │  │   (Windows)      │  │   (Linux)        │          │
│  │                  │  │                  │  │                  │          │
│  │ Web UI Config:   │  │ Web UI Config:   │  │ Web UI Config:   │          │
│  │ • Storage paths  │  │ • Storage paths  │  │ • Storage paths  │          │
│  │ • Reserved space │  │ • Reserved space │  │ • Reserved space │          │
│  │ • Server URL     │  │ • Server URL     │  │ • Server URL     │          │
│  │ • API key        │  │ • API key        │  │ • API key        │          │
│  │                  │  │                  │  │                  │          │
│  │ /mnt/storage     │  │ D:\storage       │  │ /data/oelala     │          │
│  │ [500GB reserved] │  │ [200GB reserved] │  │ [1TB reserved]   │          │
│  └──────────────────┘  └──────────────────┘  └──────────────────┘          │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## 🔑 Key Principles

### 1. Separation of Concerns

| Component | Responsibility | NOT Responsible For |
|-----------|---------------|---------------------|
| **oelala-backend** | Who can see what, retention policies, tier logic, user auth | File storage, CDN, replication |
| **Coordinator** | Node management, file placement, deduplication, CDN routing | Access control decisions, business logic |
| **Storage Nodes** | Store blobs, serve files, report health | Anything else - they are "dumb" |

### 2. Storage is "Dumb"

Storage nodes and coordinator do what they're told:
- Backend says "store this file with expiry X" → Storage stores it
- Backend says "delete this" → Storage deletes it
- Backend says "user X can access file Y" → Storage serves it

Storage does NOT:
- Decide who can see what (that's backend)
- Calculate retention periods (that's backend)
- Know about user tiers (that's backend)

### 3. Database, Not Filesystem for References

**Deduplication and references are stored in database, NOT as symlinks/hardlinks.**

```go
// Database model, NOT filesystem
type Blob struct {
    Hash      string   `json:"hash"`       // SHA-256 content hash
    Size      int64    `json:"size"`
    Nodes     []string `json:"nodes"`      // Which nodes have this blob
    RefCount  int      `json:"ref_count"`  // How many files reference this
}

type FileReference struct {
    ID        string     `json:"id"`
    UserID    string     `json:"user_id"`
    Bucket    string     `json:"bucket"`
    Key       string     `json:"key"`
    BlobHash  string     `json:"blob_hash"` // Points to Blob
    ExpiresAt *time.Time `json:"expires_at,omitempty"`
    CreatedAt time.Time  `json:"created_at"`
}
```

Why no symlinks:
- Cross-platform (Windows support)
- Database queries for analytics
- Easier redundancy tracking
- No filesystem edge cases

### 4. Single Binary, Multiple Modes

```bash
# Run as storage node (connects to coordinator)
oelala-storage serve --mode=node

# Run as coordinator (manages nodes)
oelala-storage serve --mode=coordinator

# Run as standalone (current behavior, no distribution)
oelala-storage serve --mode=standalone
```

---

## 📦 Data Model

### Blobs (Content-Addressed Storage)
```
blobs/
├── ab/cd/abcdef1234...  ← Actual file content
├── ef/01/ef0123abcd...  ← Stored by SHA-256 hash
└── ...
```

### File References (Database)
```sql
file_references:
- id: uuid
- user_id: "user-123"
- bucket: "videos"
- key: "my-video.mp4"
- blob_hash: "abcdef1234..."
- expires_at: "2026-07-15T00:00:00Z"  -- Set by backend (6 month retention)
- created_at: "2026-01-15T00:00:00Z"
```

### Node Registry (Database)
```sql
storage_nodes:
- id: uuid
- name: "flip-home-server"
- api_key_hash: "..."
- capacity_bytes: 500GB
- used_bytes: 123GB
- status: "active"
- last_heartbeat: "2026-01-15T20:30:00Z"
- region: "eu-west"
```

### Blob Locations (Database)
```sql
blob_locations:
- blob_hash: "abcdef1234..."
- node_id: "node-uuid-1"
- verified_at: "2026-01-15T20:00:00Z"
```

---

## 🔄 Retention & Garbage Collection

### Flow

1. **Backend uploads file** with `X-Expires-At` header
2. **Coordinator stores** expiration in file_reference
3. **GC job runs** periodically (hourly)
4. **Expired files**: 
   - Decrement blob refcount
   - Delete file_reference
   - If refcount=0, delete blob from nodes

### EU Compliance

- **Retention**: 6 months minimum (GDPR)
- **Account deletion**: Backend handles 2-year period + 6-month grace
- **Storage just executes**: Backend tells storage when to delete

---

## 🔗 API Responsibilities

### Backend → Coordinator

```
POST /files
  X-User-ID: user-123
  X-Expires-At: 2026-07-15T00:00:00Z
  Body: <file content>

GET /files/{bucket}/{key}
  X-User-ID: user-123
  → Coordinator checks backend for access permission

DELETE /files/{bucket}/{key}
  X-User-ID: user-123
  → Only if backend authorizes
```

### Coordinator → Nodes

```
gRPC: StoreBlob(hash, data)
gRPC: GetBlob(hash) → data
gRPC: DeleteBlob(hash)
gRPC: Heartbeat() → capacity, health
```

---

## 🖥️ Node Web UI

Each storage node has a local web UI for configuration:

```
http://localhost:7990/admin/

┌─────────────────────────────────────────────────────────────┐
│  oelala-storage Node Configuration                          │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  Status: 🟢 Connected to coordinator                         │
│                                                              │
│  ┌─────────────────────────────────────────────────────────┐│
│  │ Coordinator                                              ││
│  │ URL: https://storage.oelala.ai                          ││
│  │ API Key: oel_xxxxxxxx...                                ││
│  └─────────────────────────────────────────────────────────┘│
│                                                              │
│  ┌─────────────────────────────────────────────────────────┐│
│  │ Storage Locations                                        ││
│  │                                                          ││
│  │ Path                    Reserved    Used                ││
│  │ /mnt/storage            500 GB      234 GB  [Remove]   ││
│  │ /mnt/backup             200 GB       45 GB  [Remove]   ││
│  │                                                          ││
│  │ [+ Add Storage Location]                                ││
│  └─────────────────────────────────────────────────────────┘│
│                                                              │
│  ┌─────────────────────────────────────────────────────────┐│
│  │ Network                                                  ││
│  │ HTTP Port: 7990                                         ││
│  │ gRPC Port: 7991                                         ││
│  │ Public IP: auto-detect ○ | manual: ________            ││
│  └─────────────────────────────────────────────────────────┘│
│                                                              │
│  [Save Configuration]  [Restart Node]                       │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

---

## 📋 Implementation Phases

### Phase 1: Current (Standalone)
- [x] Single-node storage
- [x] S3-compatible API
- [x] Quota tracking
- [x] Signed URLs
- [ ] Accept `X-Expires-At` header
- [ ] GC job for expired files

### Phase 2: Coordinator Foundation
- [ ] Blob-based storage (content-addressed)
- [ ] File reference database
- [ ] Deduplication via refcount
- [ ] `--mode=coordinator` flag

### Phase 3: Node Mode
- [ ] Node registration with coordinator
- [ ] gRPC heartbeat
- [ ] Node Web UI
- [ ] `--mode=node` flag
- [ ] Windows service support

### Phase 4: Distribution
- [ ] Multi-node file placement
- [ ] Replication engine
- [ ] Cloudflare integration
- [ ] CDN routing

---

## 🔗 Related Documents

- [DISTRIBUTED_ARCHITECTURE.md](DISTRIBUTED_ARCHITECTURE.md) - Detailed distributed system design
- [ARCHITECTURE.md](ARCHITECTURE.md) - Current implementation details
- [API.md](API.md) - API reference
- [PRODUCT_VISION.md](PRODUCT_VISION.md) - Product/business vision

---

*This document is the single source of truth for oelala-storage architecture.*
