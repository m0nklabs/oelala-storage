# oelala-storage: Product Vision

> **Last Updated**: 2026-01-15
> 
> **See also**: [VISION.md](VISION.md) - Technical architecture details

---

## 🎯 Mission

**Make distributed storage accessible to everyone.**

oelala-storage is a **client/server distributed storage system** where:
- **Clients (Nodes)** = Any computer with spare disk space
- **Server (Coordinator)** = Organizes nodes, acts as CDN entry point

No cloud vendor lock-in, no per-GB fees, full control over your data.

---

## 🧩 The Problem

Current storage solutions have issues:

| Solution | Problem |
|----------|---------|
| AWS S3 / GCS / Azure | Expensive at scale, vendor lock-in |
| MinIO | Complex setup, limited distribution |
| IPFS | Too complex, unpredictable performance |
| Local disk | No redundancy, single point of failure |

For AI/ML workloads generating terabytes of media, existing solutions are either too expensive or too complex.

---

## 💡 The Solution

### Architecture: Client/Server/CDN

```
Your Backend App  →  Coordinator (CDN)  →  Storage Nodes
                           │
                   ┌───────┼───────┐
                   ▼       ▼       ▼
                 Node1   Node2   Node3
                 500GB   200GB   1TB
```

### 1. Storage Nodes (Clients)
Run on any Windows or Linux machine with spare disk space:

```bash
# Install and configure via Web UI
./oelala-storage serve --mode=node
# Then open http://localhost:7990/admin to configure
```

Web UI lets you:
- Add storage paths (e.g., `/mnt/data`, `D:\storage`)
- Set reserved space per path
- Connect to coordinator with API key

### 2. Coordinator (Server)
Manages all nodes and serves as CDN entry point:

```bash
./oelala-storage serve --mode=coordinator
```

The coordinator:
- Tracks which nodes store which files
- Routes requests to the right node
- Manages replication for redundancy
- Provides unified API for your application

### 3. API-First Design
- S3-compatible REST API for files
- gRPC for node-to-coordinator communication
- Signed URLs for secure direct access
- Easy integration with any application

### 4. Smart Features
- **Deduplication**: Same content stored once, referenced many times
- **Retention**: Automatic cleanup of expired files
- **Health monitoring**: Automatic failover when nodes go offline

---

## 👥 Target Users

### Primary: AI/ML Teams (like oelala)
- Generate large amounts of media (videos, images)
- Need affordable, scalable storage
- Want self-hosted solution
- Integrate via simple API

### Secondary: Hobbyists & Self-Hosters
- Run storage nodes on home hardware
- Contribute spare disk space
- No cloud bills, full data ownership

### Tertiary: Enterprises
- On-premise requirements
- Data sovereignty needs
- Custom deployment, multi-region

---

## 🗺️ Roadmap

### Phase 1: Standalone Mode ✅
- [x] REST API (PUT/GET/DELETE)
- [x] gRPC API skeleton
- [x] BadgerDB metadata
- [x] Filesystem storage
- [x] Bucket management
- [x] Quota tracking
- [x] Signed URLs
- [x] Web UI (Admin)

### Phase 2: Content-Addressed Storage
- [ ] Blob-based storage (store by hash)
- [ ] File references in database
- [ ] Deduplication via refcount
- [ ] `X-Expires-At` header support
- [ ] Garbage collection job

### Phase 3: Node Mode (Client)
- [ ] `--mode=node` flag
- [ ] Node Web UI for config
- [ ] gRPC registration with coordinator
- [ ] Heartbeat reporting (capacity, health)
- [ ] Windows service installer

### Phase 4: Coordinator Mode (Server)
- [ ] `--mode=coordinator` flag
- [ ] Node registry database
- [ ] File placement engine
- [ ] CDN routing logic
- [ ] Replication management

### Phase 5: Distribution & CDN
- [ ] Cloudflare integration (DNS/Tunnel)
- [ ] Global CDN caching
- [ ] Cross-region replication
- [ ] Geo-aware routing

### Phase 6: Enterprise
- [ ] SSO/SAML authentication
- [ ] Audit logging
- [ ] Compliance features (GDPR, SOC2)
- [ ] SLA guarantees

---

## 🎯 Separation of Concerns

> **CRITICAL**: oelala-storage is "dumb" storage. Your backend is the "brain".

| Responsibility | Your Backend | oelala-storage |
|---------------|--------------|----------------|
| User authentication | ✅ | ❌ |
| Access control (who sees what) | ✅ | ❌ |
| Retention policies | ✅ | ❌ (just executes) |
| Business logic | ✅ | ❌ |
| Storing files | ❌ | ✅ |
| Serving files | ❌ | ✅ |
| Deduplication | ❌ | ✅ |
| Replication | ❌ | ✅ |

### How It Works

```
Your Backend                          oelala-storage
     │                                      │
     │ POST /files                          │
     │ X-User-ID: user-123                  │
     │ X-Expires-At: 2026-07-15             │
     │ ─────────────────────────────────────→
     │                                      │ Stores file
     │                                      │ Tracks expiration
     │                                      │ Deduplicates
     │ ←─────────────────────────────────────
     │ 201 Created                          │
     │                                      │
     │ User requests file                   │
     │                                      │
     │ GET /files/videos/output.mp4         │
     │ X-User-ID: user-456                  │
     │ ─────────────────────────────────────→
     │                                      │
     │ (Storage calls back to backend)      │
     │ ←─────────────────────────────────────
     │ "Can user-456 access this?"          │
     │ ─────────────────────────────────────→
     │ "Yes" / "No"                         │
     │ ←─────────────────────────────────────
     │                                      │ Serves file (or 403)
```

---

## 💰 Business Model

### Open Source Core
- Single node: **Free forever**
- Multi-node (self-managed): **Free**
- Full source code available

### Commercial Options (Future)

| Tier | Price | Features |
|------|-------|----------|
| **Community** | Free | All modes, basic UI |
| **Pro** | $29/mo | Priority support, advanced metrics |
| **Enterprise** | Custom | SSO, audit logs, SLA, dedicated support |

---

## 🏆 Competitive Advantages

| Feature | oelala-storage | MinIO | S3 |
|---------|----------------|-------|-----|
| Self-hosted | ✅ | ✅ | ❌ |
| Simple setup | ✅ | ❌ | ✅ |
| Web UI included | ✅ | ✅ | ✅ |
| Multi-node free | ✅ | ⚠️ | ❌ |
| Per-GB cost | $0 | $0 | $$$ |
| Client/Server model | ✅ | ❌ | ❌ |
| Windows nodes | ✅ | ⚠️ | ❌ |

---

## 🔗 Integration with oelala.xyz

oelala-storage was built for oelala.xyz but is designed as a **standalone product**:

```
┌─────────────────────────────────────────────────────────────────┐
│                        oelala.xyz                               │
│                    (AI Video Generation)                        │
├─────────────────────────────────────────────────────────────────┤
│                              │                                  │
│   oelala-backend ───────────────────────→  oelala-storage       │
│   (Python/FastAPI)          │              (Coordinator)        │
│                             │                    │              │
│   • User auth               │                    │              │
│   • Access control          │              ┌─────┴─────┐        │
│   • Retention logic         │              ▼           ▼        │
│   • X-Expires-At headers    │           Node 1      Node 2      │
│                             │                                   │
│                                                                 │
│   Can be used by ANY application:                               │
│   • Other AI tools                                              │
│   • Media platforms                                             │
│   • Archive systems                                             │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### oelala.xyz Integration Points

1. **Video output storage** - Generated videos stored in user buckets
2. **Image uploads** - User-uploaded reference images  
3. **Auto-upload** - ComfyUI outputs automatically stored
4. **CDN delivery** - Fast global access via coordinator
5. **Retention** - 6-month EU compliance via `X-Expires-At`

---

## 🎨 Brand Identity

### Name
**oelala-storage** - Playful, memorable, connected to parent brand

### Tagline Options
- "Storage that scales with you"
- "Your files, your hardware, your rules"
- "Distributed storage made simple"

### Logo
Simple, clean, represents distributed/connected nodes

---

## 📈 Success Metrics

### Year 1 Goals
- 1,000+ GitHub stars
- 100+ active deployments
- 10 enterprise customers
- $50k ARR

### Key Metrics
- Downloads per month
- Active deployments (phone home optional)
- Community engagement (GitHub issues, Discord)
- Conversion to paid tiers

---

## 🚀 Next Steps

1. **Complete API key management** - Users can generate keys via Web UI
2. **Build Web UI** - Embedded admin panel
3. **Document API** - OpenAPI spec, examples
4. **Launch on Product Hunt** - Get early adopters
5. **Build community** - Discord, GitHub discussions

---

*This is a living document. Updated as the vision evolves.*
