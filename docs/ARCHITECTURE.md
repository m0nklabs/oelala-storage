# System Architecture

This document describes the current architecture of `oelala-storage` as of March 2026.

## Overview

`oelala-storage` is a Go-based object storage service focused on media-heavy workloads. It provides a simple HTTP API, metadata + dedup tracking, signed/public access patterns, retention execution, and the foundation for a multi-node storage network.

The design principle is intentionally simple:

> The backend decides policy. Storage executes object operations reliably.

## Design Goals

1. **Storage stays dumb**: no business logic about user tiers or product behavior
2. **Media-friendly**: large uploads, ranged reads, cache headers, content-disposition
3. **Cross-platform**: Windows and Linux support only
4. **Node-aware**: ready for coordinator + replica topology
5. **Auditable**: metrics, metering, webhooks, admin surfaces
6. **Composable**: easy to integrate from other services over HTTP

## High-Level Topology

```
Application / Backend
      │
      │  Authorization: Bearer <token>
      ▼
┌──────────────────────────────────────────────────────────────┐
│                  oelala-storage HTTP API                    │
│                         Port 7990                           │
├──────────────────────────────────────────────────────────────┤
│ Middleware                                                  │
│  • auth / permission checks                                 │
│  • CORS                                                     │
│  • metrics                                                  │
│  • request logging / recovery                               │
├──────────────────────────────────────────────────────────────┤
│ Handlers                                                    │
│  • PUT / GET / HEAD / DELETE / LIST                         │
│  • POST move                                                │
│  • signed URL validation                                    │
│  • admin + status endpoints                                 │
├──────────────────────────────────────────────────────────────┤
│ Core Storage                                                │
│  • filesystem object store                                  │
│  • Badger metadata                                          │
│  • dedup references                                         │
│  • quota / metering hooks                                   │
│  • retention / GC                                           │
│  • webhook dispatch                                         │
├──────────────────────────────────────────────────────────────┤
│ Distributed Layer                                           │
│  • gRPC sync                                                │
│  • replication                                              │
│  • coordinator heartbeat client                             │
│  • future node routing / placement                          │
└──────────────────────────────────────────────────────────────┘
```

## Runtime Components

### HTTP API

**Technology**: Fiber

**Responsibilities**:
- upload/download/delete/head/list objects
- move/rename objects via POST action
- apply auth and permission checks
- expose status/admin endpoints
- handle Range requests and cache-related headers

### Metadata + Dedup

**Technology**: BadgerDB + filesystem

**Responsibilities**:
- map logical bucket/key references to physical blobs
- maintain reference counts for content-addressed storage
- support safe delete/move semantics
- store metadata needed for listing, retention, and signed access

### GC / Retention

Retention is driven by upstream services through `X-Expires-At` metadata.

Storage responsibilities:
- persist expiration metadata
- expose GC stats/status
- delete expired objects when collected

### Signed URLs

Signed URLs provide time-limited access for public/share scenarios without turning the whole bucket public.

### Webhooks

Webhook notifications are emitted for events such as:
- `file.uploaded`
- `file.deleted`
- `quota.warning`
- `quota.exceeded`
- `file.expiring`
- `gc.completed`

Webhook delivery uses async dispatch, retry/backoff, and HMAC-SHA256 signing.

## Authentication Model

### Object API

Normal application access uses:

```http
Authorization: Bearer <token>
```

This can represent either an application/service token or another approved bearer token format handled by the auth middleware.

### Admin API

Administrative surfaces use:

```http
X-Admin-Secret: <secret>
```

### Permission Model

The HTTP layer distinguishes between reader and writer capabilities:
- `GET`, `HEAD`, `LIST` require read-style permission
- `PUT`, `DELETE`, `POST action=move` require write-style permission

Compatibility aliases for `read/write` and `reader/writer` are supported.

## Storage Operations

### Upload Path

1. Authenticate request
2. Parse bucket and object key
3. Detect content type if needed
4. Store object bytes
5. Update metadata / dedup references
6. Update metering / quota state
7. Emit webhook where applicable

### Read Path

1. Authenticate request or validate signed URL
2. Resolve bucket/key metadata
3. Serve full or ranged content
4. Attach cache, ETag, and content-disposition headers as applicable
5. Record metrics/metering

### Move Path

1. Authenticate write access
2. Resolve source object
3. Move logical reference and/or filesystem object
4. Preserve dedup correctness when overwriting or re-pointing references

## Distributed Direction

The current architecture is evolving from a single primary storage service toward a coordinator-plus-node model.

### Active Pieces
- coordinator client with heartbeat payloads
- node config including `public_url`
- gRPC sync infrastructure
- static peer sync support

### Current Real-World Shape

| Node | Role | Notes |
|------|------|-------|
| storage-main | coordinator / primary | main public entrypoint |
| storage-node-01 | local extra node | separate local service + ports |
| storage-node-02 | remote node | independent tunnel and host |

## Observability

Metrics are exposed on the configured metrics port.

Important signals include:
- request counts and latency
- object totals and storage bytes
- sync activity
- quota state
- GC and dedup statistics

## Platform Policy Notes

- Supported platforms: **Windows and Linux only**
- Android, macOS, iOS are out of scope
- Storage should not absorb backend-only concerns such as user management or subscription logic
  "created_at": "2026-01-12T10:30:00Z"
}
```

**Operations:**
- Put metadata
- Get metadata
- Delete metadata
- Scan by prefix (for listing)

### Quota Management

**Package:** `internal/quota`

**Tiers and Limits:**

| Tier    | Storage | Retention  |
|---------|---------|------------|
| Free    | 2 GB    | 30 days    |
| Creator | 50 GB   | Unlimited  |
| Pro     | 200 GB  | Unlimited  |
| Studio  | 1 TB    | Unlimited  |

**Quota Tracker:**
```go
type Usage struct {
    UserID      string
    TierID      string
    UsedBytes   int64
    ObjectCount int64
    QuotaBytes  int64
}
```

**Operations:**
- Track storage usage per user
- Enforce quota limits
- Update on upload/delete
- Get usage statistics

**Future Enhancements:**
- Persistent usage tracking (currently in-memory)
- Quota alerts/warnings
- Automatic cleanup for expired files (free tier)

### Usage Metering

**Package:** `internal/metering`

**Event Types:**
- `storage.upload` - Bytes stored
- `storage.download` - Bandwidth used
- `storage.delete` - Bytes freed
- `generation.image` - Image generation (1 credit)
- `generation.video` - Video generation (5-20 credits)
- `generation.audio` - Audio generation (2 credits)

**Event Structure:**
```go
type Event struct {
    ID        string
    UserID    string
    EventType EventType
    Bytes     int64
    Credits   int
    Timestamp time.Time
    Metadata  map[string]string
}
```

**Usage:**
- Real-time event recording
- Event handlers for forwarding to billing system
- In-memory buffer (10,000 events)
- Circular buffer (FIFO eviction)

**Future Enhancements:**
- Persistent event storage
- Export to analytics platforms
- Aggregation and reporting

### Peer Synchronization

**Package:** `internal/sync`

**Protocol:** gRPC (Port 7991)

**Components:**

#### 1. Peer Discovery (mDNS)
- Automatic peer discovery on local network
- Broadcast service availability
- Listen for peer announcements
- Filter by service type (`_oelala-storage._tcp`)

#### 2. Replication Engine
- Push/pull synchronization
- Incremental sync (only changed objects)
- Conflict resolution (last-write-wins)
- Bandwidth throttling
- Background sync workers

**Sync Strategies:**
- **Realtime**: Immediate sync on changes
- **Scheduled**: Periodic sync (configurable interval)
- **Manual**: User-triggered sync

**Proto Definitions:**
```protobuf
service SyncService {
  rpc ListObjects(ListObjectsRequest) returns (ListObjectsResponse);
  rpc GetObject(GetObjectRequest) returns (stream ObjectChunk);
  rpc PutObject(stream ObjectChunk) returns (PutObjectResponse);
  rpc DeleteObject(DeleteObjectRequest) returns (DeleteObjectResponse);
}
```

**Future Enhancements:**
- Wide-area network sync (beyond local network)
- Selective sync (folder-based)
- Conflict detection and manual resolution
- Delta sync (binary diffs)

### Observability

#### Logging (Zap)

**Package:** `internal/logging`

**Features:**
- Structured logging (JSON or console)
- Log levels: debug, info, warn, error
- Context-aware logging
- Performance-optimized

**Log Fields:**
- Timestamp
- Level
- Message
- User ID
- Request ID
- Duration
- Error details

#### Metrics (Prometheus)

**Package:** `internal/metrics`

**Port:** 7992

**Metrics Collected:**
- Request count by method/path/status
- Request duration (histogram)
- Upload/download bytes
- Active connections
- Storage usage
- Quota utilization
- Peer count

**Endpoints:**
- `/metrics` - Prometheus scrape endpoint

**Example Metrics:**
```
# Request count
http_requests_total{method="PUT",path="/users/:id/media/:key",status="201"} 42

# Request duration
http_request_duration_seconds_bucket{method="GET",le="0.1"} 100

# Storage bytes
storage_bytes_total{tier="free"} 2147483648
```

### TLS/SSL

**Package:** `internal/tls`

**Features:**
- Optional TLS support
- Auto-generate self-signed certificates
- Custom certificate support
- Certificate validation

**Configuration:**
```yaml
tls:
  enabled: true
  auto_cert: true  # Generate self-signed if missing
  cert_file: /path/to/cert.pem
  key_file: /path/to/key.pem
```

### Configuration

**Package:** `internal/config`

**Technology:** Viper

**Config File Locations:**
1. `./oelala-storage.yaml`
2. `~/.config/oelala-storage/oelala-storage.yaml`
3. `/etc/oelala-storage/oelala-storage.yaml`

**Environment Variables:**
Prefix: `OELALA_STORAGE_`

Example: `OELALA_STORAGE_API_HTTP_PORT=8080`

**Example Configuration:**
```yaml
node:
  id: auto
  name: primary-node
  type: primary  # primary, replica, edge, archive

storage:
  path: ./data
  max_size_gb: 500
  cache_size_mb: 2048

api:
  http_port: 7990
  grpc_port: 7991
  enable_tls: false

sync:
  enabled: true
  strategy: realtime  # realtime, scheduled, manual
  interval_minutes: 15
  peers:
    - url: http://peer1:7990
      type: replica

security:
  encryption_at_rest: false
  auth_tokens:
    - name: service-account
      token: secret-token-here
      permissions: [read, write, delete]

logging:
  level: info  # debug, info, warn, error
  format: console  # console, json

metrics:
  enabled: true
  port: 7992

tls:
  enabled: false
  auto_cert: true
  cert_file: ""
  key_file: ""
```

## Data Flow

### Upload Flow

```
Client (oelala backend)
    │
    │ PUT /users/{id}/media/video.mp4
    │ Authorization: Bearer <jwt>
    │ Content-Type: video/mp4
    │ Body: <binary data>
    │
    ▼
┌────────────────────────────────┐
│  HTTP Server (Fiber)           │
│  - Receive request             │
│  - Buffer body (100GB max)     │
└────────┬───────────────────────┘
         │
         ▼
┌────────────────────────────────┐
│  Auth Middleware               │
│  - Validate JWT                │
│  - Extract user context        │
└────────┬───────────────────────┘
         │
         ▼
┌────────────────────────────────┐
│  Quota Middleware (future)     │
│  - Check storage quota         │
│  - Return 402 if exceeded      │
└────────┬───────────────────────┘
         │
         ▼
┌────────────────────────────────┐
│  Storage Layer                 │
│  1. Compute SHA-256 hash       │
│  2. Detect content type        │
│  3. Write file to disk         │
│  4. Update BadgerDB metadata   │
│  5. Update quota tracker       │
│  6. Record metering event      │
└────────┬───────────────────────┘
         │
         ▼
┌────────────────────────────────┐
│  Response                      │
│  201 Created                   │
│  {                             │
│    "hash": "sha256:...",       │
│    "size": 1048576,            │
│    "content_type": "video/mp4" │
│  }                             │
└────────────────────────────────┘
```

### Download Flow

```
Client
    │
    │ GET /users/{id}/media/video.mp4
    │ Authorization: Bearer <jwt>
    │
    ▼
┌────────────────────────────────┐
│  HTTP Server (Fiber)           │
└────────┬───────────────────────┘
         │
         ▼
┌────────────────────────────────┐
│  Auth Middleware               │
└────────┬───────────────────────┘
         │
         ▼
┌────────────────────────────────┐
│  Storage Layer                 │
│  1. Lookup metadata in BadgerDB│
│  2. Get file hash              │
│  3. Open file from disk        │
│  4. Record download event      │
└────────┬───────────────────────┘
         │
         ▼
┌────────────────────────────────┐
│  Stream Response               │
│  200 OK                        │
│  Content-Type: video/mp4       │
│  Content-Length: 1048576       │
│  Body: <stream binary data>    │
└────────────────────────────────┘
```

### Sync Flow

```
Node A                          Node B
  │                               │
  │ 1. mDNS Discovery            │
  │◄─────────────────────────────▶│
  │                               │
  │ 2. Establish gRPC connection │
  │──────────────────────────────▶│
  │                               │
  │ 3. ListObjects (get hashes)  │
  │──────────────────────────────▶│
  │◄──────────────────────────────│
  │        [list of objects]      │
  │                               │
  │ 4. Compare hashes             │
  │   (find missing objects)      │
  │                               │
  │ 5. GetObject (stream data)   │
  │──────────────────────────────▶│
  │◄──────────────────────────────│
  │        [object chunks]        │
  │                               │
  │ 6. Store locally              │
  │   (compute hash, save)        │
  │                               │
  │ 7. Acknowledge                │
  │──────────────────────────────▶│
  │                               │
```

## Security Considerations

### Authentication
- JWT validation with signature verification
- API key support for service accounts
- Token expiration checking
- Constant-time comparison for keys

### Authorization
- User isolation (users can only access their own files)
- Role-based access control (future)
- Tier-based feature gating

### Data Protection
- Content-addressed storage (integrity verification)
- SHA-256 hashing for deduplication and verification
- Optional TLS for data in transit
- Optional encryption at rest (future)

### Attack Prevention
- Request size limits (100GB max)
- Rate limiting (future)
- Input validation
- Path traversal prevention
- CORS configuration

## Performance Characteristics

### Throughput
- **Upload**: Limited by disk I/O (~100-500 MB/s on SSD)
- **Download**: Limited by network bandwidth
- **Metadata ops**: ~10,000 ops/sec (BadgerDB)

### Latency
- **Small files (<1MB)**: <50ms
- **Large files (>100MB)**: Streaming (no buffering)
- **Metadata lookup**: <5ms

### Scalability
- **Single node**: 500GB-1TB storage target
- **Concurrent connections**: 1000+
- **Objects**: Millions (BadgerDB scales well)

### Resource Usage
- **Memory**: ~50-100MB base + cache
- **CPU**: Low (I/O bound)
- **Disk**: Content + metadata (~1% overhead)

## Deployment Architecture

### Single Node (Development)

```
┌─────────────────────────────┐
│   Developer Machine         │
│                             │
│  ┌──────────────────────┐   │
│  │  oelala-storage      │   │
│  │  :7990 :7991 :7992   │   │
│  └──────────────────────┘   │
│                             │
│  ┌──────────────────────┐   │
│  │  ./data/             │   │
│  │  - objects/          │   │
│  │  - metadata.db/      │   │
│  └──────────────────────┘   │
└─────────────────────────────┘
```

### Multi-Device (User Setup)

```
┌──────────────────┐         ┌──────────────────┐
│  Desktop PC      │         │  Laptop          │
│                  │         │                  │
│  oelala-storage  │◄───────▶│  oelala-storage  │
│  :7990 :7991     │  Sync   │  :7990 :7991     │
│                  │ (gRPC)  │                  │
│  ~/storage/      │         │  ~/storage/      │
└──────────────────┘         └──────────────────┘
         │                            │
         │                            │
         │      Sync (gRPC)           │
         │                            │
         └────────────┬───────────────┘
                      │
                      ▼
         ┌──────────────────────┐
         │  Android Device      │
         │                      │
         │  oelala-storage      │
         │  :7990 :7991         │
         │                      │
         │  /storage/           │
         └──────────────────────┘
```

### Production (Future - Optional Cloud Tier)

```
                  Internet
                     │
                     ▼
         ┌───────────────────────┐
         │  CloudFlare CDN       │
         │  (Optional)           │
         └───────────┬───────────┘
                     │
         ┌───────────▼───────────┐
         │  Load Balancer        │
         └───────────┬───────────┘
                     │
         ┌───────────┴───────────┐
         │                       │
    ┌────▼────┐            ┌─────▼────┐
    │ Node 1  │            │  Node 2  │
    │ Primary │◄──Sync────▶│ Replica  │
    └────┬────┘            └─────┬────┘
         │                       │
    ┌────▼────┐            ┌─────▼────┐
    │ Storage │            │ Storage  │
    │ (SSD)   │            │ (SSD)    │
    └─────────┘            └──────────┘
```

## Technology Stack

### Core
- **Language:** Go 1.21+
- **HTTP Framework:** GoFiber v2
- **CLI Framework:** Cobra
- **Configuration:** Viper

### Storage
- **Metadata:** BadgerDB v4
- **File System:** OS native

### Networking
- **RPC:** gRPC
- **Discovery:** mDNS

### Observability
- **Logging:** Zap
- **Metrics:** Prometheus

### Security
- **Auth:** JWT validation
- **TLS:** Go crypto/tls

### Build
- **Build Tool:** Make
- **Cross-compile:** GOOS/GOARCH
- **Target Platforms:** Linux, Windows, Android

## Future Enhancements

### Phase 1 (Current) ✅
- Basic S3-compatible API
- File storage with SHA-256
- BadgerDB metadata
- Authentication (JWT/API key)
- Quota tracking
- Usage metering

### Phase 2 (In Progress)
- Peer synchronization (gRPC)
- Peer discovery (mDNS)
- Replication engine
- TLS support
- Prometheus metrics

### Phase 3 (Planned)
- Persistent quota tracking
- Retention policies (30-day cleanup for free tier)
- Signed URLs (temporary sharing)
- Content deduplication
- Encryption at rest

### Phase 4 (Future)
- Multi-region sync
- CDN integration
- S3/R2 backend tier
- Advanced conflict resolution
- Chunked uploads (resumable)
- Bandwidth throttling per tier
- API rate limiting

## Troubleshooting

### High Memory Usage
- Reduce cache size (`storage.cache_size_mb`)
- Check for memory leaks in long-running processes
- Monitor with `/metrics` endpoint

### Slow Uploads
- Check disk I/O (use SSD if possible)
- Verify network bandwidth
- Reduce concurrent uploads
- Check CPU usage during hash computation

### Sync Issues
- Verify peers are on same network (mDNS)
- Check firewall rules (port 7991)
- Enable debug logging (`logging.level: debug`)
- Check gRPC connection status

### Quota Not Enforced
- Currently in-memory only (resets on restart)
- Future: persistent quota tracking
- Workaround: Check quota in backend before upload

## Monitoring

### Key Metrics to Watch

1. **Storage Usage**
   - `storage_bytes_total{tier="free"}`
   - `storage_objects_total{tier="free"}`

2. **Request Performance**
   - `http_request_duration_seconds`
   - `http_requests_total`

3. **Error Rate**
   - `http_requests_total{status="5xx"}`
   - `http_requests_total{status="4xx"}`

4. **Sync Health**
   - `sync_peers_connected`
   - `sync_objects_replicated`

### Health Checks

```bash
# Application health
curl http://localhost:7990/health

# Metrics
curl http://localhost:7992/metrics

# Service status
curl http://localhost:7990/status
```

## References

- [INTEGRATION.md](./INTEGRATION.md) - Backend integration guide
- [API.md](./API.md) - Complete API reference
- [CONTRIBUTING.md](../CONTRIBUTING.md) - Development guide
- [AGENT_CONTEXT.md](../AGENT_CONTEXT.md) - Project context
