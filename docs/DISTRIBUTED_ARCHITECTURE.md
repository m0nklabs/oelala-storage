# oelala-storage: Distributed Architecture

> **Status**: ACTIVE DIRECTION - partially implemented architecture for the storage network
> **Last Updated**: 2026-03-06

---

## 🎯 Vision

oelala-storage is NOT just a storage service - it's a **distributed file network** where:

1. **Operators can run storage nodes** on their own hardware (Windows/Linux)
2. **A coordinator** manages node registration, file placement, and redundancy
3. **Cloudflare** provides global access to stored content
4. **Files are replicated** 2x or 3x across nodes for durability

Think: coordinator-managed object storage with independent nodes, not a pure peer-to-peer swarm.

---

## 🏗️ Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              COORDINATOR                                     │
│                         (oelala main server)                                 │
│                                                                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │   Node      │  │   File      │  │  Cloudflare │  │    oelala API       │ │
│  │  Registry   │  │  Placement  │  │  Integration│  │   (existing)        │ │
│  │             │  │   Engine    │  │             │  │                     │ │
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────────────┘ │
│         │               │                │                    │             │
└─────────┼───────────────┼────────────────┼────────────────────┼─────────────┘
          │               │                │                    │
          │ gRPC          │ Placement      │ DNS/CDN            │ HTTP API
          │ Heartbeat     │ Decisions      │                    │
          ▼               ▼                ▼                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                              CLOUDFLARE                                      │
│                                                                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────────────────┐  │
│  │  DNS/CDN    │  │   R2        │  │  Tunnel (cloudflared)               │  │
│  │  caching    │  │  (optional) │  │  for node access                    │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────────────────┘  │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
          │                                        │
          │ HTTPS                                  │ Tunnel
          │                                        │
          ▼                                        ▼
┌──────────────────┐   ┌──────────────────┐   ┌──────────────────┐
│   STORAGE NODE   │   │   STORAGE NODE   │   │   STORAGE NODE   │
│   (main/local)   │   │   (node-01)      │   │   (node-02)      │
│                  │   │                  │   │                  │
│  ┌────────────┐  │   │  ┌────────────┐  │   │  ┌────────────┐  │
│  │ File Store │  │   │  │ File Store │  │   │  │ File Store │  │
│  │ /mnt/data  │  │   │  │ D:\storage │  │   │  │ /home/data │  │
│  └────────────┘  │   │  └────────────┘  │   │  └────────────┘  │
│                  │   │                  │   │                  │
│  Windows/Linux   │   │  Windows         │   │  Linux           │
│  500GB-2TB       │   │  100GB           │   │  1TB             │
└──────────────────┘   └──────────────────┘   └──────────────────┘
```

---

## 🔧 Components

### 1. Storage Node (oelala-storage binary)

A lightweight Go binary that:
- **Runs as a service** (Windows Service / systemd)
- **Connects to coordinator** with API key
- **Stores files** on local disk
- **Reports health** via gRPC heartbeat
- **Serves files** via HTTP (through Cloudflare tunnel or direct)

#### Installation Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                     NODE INSTALLATION                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. Download binary                                              │
│     └─ Windows: oelala-storage.exe                              │
│     └─ Linux: oelala-storage                                    │
│                                                                  │
│  2. Run setup wizard                                             │
│     └─ oelala-storage setup                                     │
│                                                                  │
│  3. Configure:                                                   │
│     ┌─────────────────────────────────────────────────────────┐ │
│     │  Storage path: D:\oelala-storage                        │ │
│     │  Coordinator:  https://api.oelala.xyz                   │ │
│     │  API Key:      oel_xxxxxxxxxxxxxxxxxxxx                 │ │
│     │  Max storage:  500 GB                                   │ │
│     │  Public IP:    (auto-detected or manual)                │ │
│     └─────────────────────────────────────────────────────────┘ │
│                                                                  │
│  4. Install as service                                           │
│     └─ oelala-storage install                                   │
│                                                                  │
│  5. Node registers with coordinator                              │
│     └─ Appears in oelala dashboard as "pending"                 │
│                                                                  │
│  6. Admin approves node                                          │
│     └─ Node status: "active"                                    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

#### Config File (oelala-storage.yaml)

```yaml
# Storage Node Configuration
node:
  name: "flip-pc-node"
  storage_path: "/mnt/ssd/oelala-storage"
  max_storage_gb: 500
  
coordinator:
   url: "https://api.oelala.xyz"
  api_key: "oel_xxxxxxxxxxxxxxxxxxxx"
  
network:
  http_port: 7990
  grpc_port: 7991
  public_ip: "auto"  # or specific IP
  cloudflare_tunnel: true  # Use cloudflared for access
  
replication:
  accept_replicas: true   # Accept files from other nodes
  max_bandwidth_mbps: 100  # Limit replication bandwidth
```

### 2. Coordinator (part of oelala backend)

The coordinator runs as part of the main oelala backend and:
- **Registers nodes** and tracks their status
- **Decides file placement** based on capacity and location
- **Manages replication** (ensures 2x/3x copies exist)
- **Tracks Cloudflare-facing node URLs** and tunnel topology
- **Routes requests** to correct nodes

#### Node Registry (Supabase table)

```sql
CREATE TABLE storage_nodes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    api_key_hash TEXT NOT NULL,  -- bcrypt hash of API key
    
    -- Network
    public_ip TEXT,
    cloudflare_tunnel_id TEXT,
    http_port INTEGER DEFAULT 7990,
    grpc_port INTEGER DEFAULT 7991,
    
    -- Capacity
    total_bytes BIGINT,
    used_bytes BIGINT,
    max_files INTEGER,
    
    -- Status
    status TEXT DEFAULT 'pending',  -- pending, active, offline, suspended
    last_heartbeat TIMESTAMPTZ,
    region TEXT,  -- eu-west, us-east, etc.
    
    -- Owner
    owner_user_id UUID REFERENCES auth.users(id),
    
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);
```

#### File Placement Table

```sql
CREATE TABLE file_placements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    file_hash TEXT NOT NULL,       -- SHA-256 of content
    bucket TEXT NOT NULL,
    key TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    
    -- Placement
    primary_node_id UUID REFERENCES storage_nodes(id),
    replica_node_ids UUID[],       -- Array of replica node IDs
    replication_factor INTEGER DEFAULT 2,
    
    -- Status
    status TEXT DEFAULT 'placing', -- placing, healthy, degraded
    
    created_at TIMESTAMPTZ DEFAULT now()
);

-- Index for lookups
CREATE INDEX idx_file_placements_hash ON file_placements(file_hash);
CREATE INDEX idx_file_placements_bucket_key ON file_placements(bucket, key);
```

### 3. Cloudflare Integration

Each storage node can be accessed via:

**Option A: Cloudflare Tunnel (cloudflared)**
- Node runs `cloudflared` alongside storage service
- No port forwarding needed
- Coordinator assigns or records node-specific hostnames

**Option B: Direct with Cloudflare CDN**
- Node has public IP with port forwarding
- Coordinator adds DNS record pointing to node
- Cloudflare caches content

```
Request Flow:
                                                    
   User → storage.oelala.xyz/user/video.mp4
           │                                        
           ▼                                        
       Cloudflare                                   
           │                                        
           ├─── Cache hit? → Return cached          
           │                                        
           ▼ Cache miss                             
       Coordinator                                  
           │                                        
           │ Lookup file_placements                 
           │ Get node with lowest latency           
           │                                        
           ▼                                        
       Storage Node                                 
           │                                        
           ▼                                        
       Return file → Cache at edge                  
```

---

## Current Reality

Pieces already implemented or actively underway:

- coordinator client + heartbeat payloads in the storage service
- node config with `public_url`
- static-peer sync and replication-related improvements
- storage admin visibility in Oelala frontend/backend

Pieces still evolving:

- placement policy
- full health and lag visibility across nodes
- automated replication guarantees by tier
- cleaner public hostname strategy across coordinator and nodes

## 📦 Replication

### Replication Factor

| User Tier | Replication | Meaning |
|-----------|-------------|---------|
| Free | 1x | Single copy (node failure = data loss) |
| Creator | 2x | Two copies on different nodes |
| Pro | 2x | Two copies on different nodes |
| Studio | 3x | Three copies, geo-distributed |

### Replication Process

```
1. User uploads file via oelala API
   └─ POST /api/v1/media/upload

2. Coordinator receives file
   └─ Calculate SHA-256 hash
   └─ Check deduplication (hash exists?)

3. Select primary node
   └─ Based on: capacity, location, health
   └─ Prefer: user's own node if available

4. Upload to primary node
   └─ gRPC: PutFile(hash, data)
   └─ Primary confirms storage

5. Create file_placement record
   └─ status: "placing"

6. Async: Replicate to N-1 other nodes
   └─ Background job
   └─ Node-to-node gRPC transfer
   └─ Update replica_node_ids[]

7. Mark placement healthy
   └─ status: "healthy"
```

### Degraded State Handling

```
Monitor job runs every 5 minutes:

1. Check each file_placement
   └─ Query node health for primary + replicas

2. If primary offline:
   └─ Promote replica to primary
   └─ Find new replica node
   └─ status: "degraded" → "repairing"

3. If replica count < replication_factor:
   └─ Find available node
   └─ Replicate from healthy node
   └─ Add to replica_node_ids[]

4. When replica count restored:
   └─ status: "healthy"
```

---

## 🔐 Security

### API Keys

- Generated via oelala dashboard
- Scoped to specific node
- Can be revoked
- Format: `oel_node_<random32chars>`

### Node Authentication

```
Node → Coordinator:
  Header: X-Node-API-Key: oel_node_xxxx
  
Coordinator verifies:
  1. API key exists in storage_nodes
  2. Status is not 'suspended'
  3. IP matches (optional strict mode)
```

### File Access

Public files:
- Served directly via Cloudflare/CDN-facing storage hostname
- No authentication needed
- URL: `storage.oelala.xyz/{bucket}/{key}`

Private files:
- Require signed URL from coordinator
- URL contains HMAC signature + expiry
- Node validates signature before serving

---

## 🚀 MVP Scope

### Phase 1: Single Node + Coordinator Skeleton
- [ ] Node setup wizard (storage path, API key)
- [ ] Windows service + systemd unit
- [ ] gRPC heartbeat to coordinator
- [ ] Coordinator node registry (Supabase)
- [ ] Basic health dashboard

### Phase 2: Multi-Node + Replication
- [ ] File placement table
- [ ] Upload routing to nodes
- [ ] Node-to-node replication (gRPC)
- [ ] Degraded state recovery
- [ ] Replication dashboard

### Phase 3: Cloudflare Integration
- [ ] cloudflared tunnel setup (automatic)
- [ ] DNS management via Cloudflare API
- [ ] CDN caching for public files
- [ ] Signed URLs for private files

### Phase 4: User-Owned Nodes
- [ ] "Contribute storage" UI in oelala
- [ ] Reward system for contributors
- [ ] Node performance metrics
- [ ] Fair scheduling across nodes

---

## 📊 Metrics & Monitoring

Each node reports:
- Storage used/available
- Files stored
- Bandwidth in/out
- Request latency
- Replication lag
- Health status

Coordinator aggregates:
- Total network capacity
- Replication health
- Geographic distribution
- Node availability SLA

---

## 🆚 Alternative Approaches Considered

| Approach | Pros | Cons | Decision |
|----------|------|------|----------|
| Pure S3/R2 | Simple, managed | Vendor lock-in, costs scale | ❌ |
| IPFS | Decentralized | Complex, slow, no guarantees | ❌ |
| MinIO cluster | S3-compatible | Heavy, needs infra | ❌ |
| Custom (this) | Control, costs, fun | Dev effort | ✅ |

---

## 🔗 Related Documents

- [MEDIA_STORAGE.md](../oelala/docs/MEDIA_STORAGE.md) - Integration with oelala
- [MONETIZATION.md](../oelala/docs/MONETIZATION.md) - Tier-based features
- [oelala-storage AGENT_CONTEXT.md](./AGENT_CONTEXT.md) - Dev context

---

## 📝 Open Questions

1. **How to handle NAT/firewall on user nodes?**
   - Option A: Require cloudflared (recommended)
   - Option B: UPnP auto-forward
   - Option C: STUN/TURN relay

2. **Node incentives for non-paying users?**
   - Free storage credit for contributed space?
   - "Pro features" unlock for node operators?

3. **What if a node goes offline with unreplicated files?**
   - Grace period before marking lost
   - User notification
   - Priority re-upload prompt

4. **Geographic placement strategy?**
   - Prefer same-region for latency
   - Require cross-region for Studio tier redundancy
