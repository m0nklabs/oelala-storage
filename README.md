# oelala-storage

Distributed object storage for AI-generated media, built for self-hosted and multi-node deployments.

---

## What it is

`oelala-storage` is a standalone Go service that provides:

- S3-style object operations over a simple HTTP API
- content-addressed deduplication with metadata tracking
- quota and metering hooks for upstream applications
- retention execution via `X-Expires-At`
- signed URLs for controlled public access
- webhook notifications for storage events
- multi-node coordination and replication groundwork

It started as the storage backend for Oelala, but the design goal is broader: a reusable storage service for media-heavy applications.

---

## Current Deployment Shape

| Node | Role | Hostname | Ports | Data Path |
|------|------|----------|-------|-----------|
| storage-main | coordinator / primary | `storage-main.oelala.xyz` | HTTP 7990, gRPC 7991, metrics 7992 | `/home/flip/oelala-main-data` or active configured path |
| storage-node-01 | additional local node | `storage-node-01.oelala.xyz` | HTTP 7993, gRPC 7994, metrics 7995 | `/home/flip/oelala-storage-data` |
| storage-node-02 | remote node | `storage2.oelala.xyz` | node-local config | remote host data path |

Each node is expected to operate independently, including its own Cloudflare tunnel where applicable.

---

## Quick Start

### Build

```bash
go build -o bin/oelala-storage ./cmd/oelala-storage
```

### Run

```bash
./bin/oelala-storage serve
```

### Example Configuration

```yaml
node:
  id: "oelala-main"
  name: "Oelala Primary Storage"
  type: primary
  public_url: "https://storage-main.oelala.xyz"

storage:
  path: "/home/flip/oelala-storage-data"
  max_size_gb: 500
  cache_size_mb: 2048

api:
  http_port: 7990
  grpc_port: 7991
  enable_tls: false

metrics:
  enabled: true
  port: 7992

security:
  admin_secret: "change-me"
  signing_secret: "change-me"
  auth_tokens:
    - name: "backend"
      token: "change-me"
      permissions: ["read", "write", "delete"]

coordinator:
  enabled: true
  url: "http://localhost:7998"
  api_key: "change-me"
  heartbeat_secs: 60
```

---

## Architecture Highlights

```
Client / Backend
      │
      ▼
HTTP API (:7990 / node-local equivalent)
  ├─ Auth / permission middleware
  ├─ Object handlers (PUT, POST move, GET, DELETE, HEAD, LIST)
  ├─ Range / cache / content-disposition support
  ├─ Signed URL validation
  └─ Webhook dispatch

Storage Core
  ├─ Filesystem object store
  ├─ Badger-backed metadata and dedup references
  ├─ Quota + metering updates
  ├─ Retention / GC execution
  └─ Metrics

Distributed Layer
  ├─ gRPC sync
  ├─ peer replication
  ├─ coordinator heartbeat/client
  └─ future placement + node routing
```

---

## Key Features

### Object Storage
- `PUT /:bucket/*` upload
- `GET /:bucket/*` download with Range support
- `HEAD /:bucket/*` metadata lookup
- `DELETE /:bucket/*` delete
- `GET /:bucket?list=true` listing with pagination support
- `POST /:bucket/*?action=move` move/rename objects

### Data Management
- content-addressed deduplication
- garbage collection of expired content
- retention metadata via `X-Expires-At`
- signed URLs for public/shareable reads

### Operations
- Prometheus metrics
- admin API / UI hooks
- webhook notifications for file and quota events
- self-hosted runner-friendly CI/CD

---

## Authentication

### Application/API access

Use a bearer token:

```http
Authorization: Bearer <token>
```

This is the canonical auth header for storage clients and backend integrations.

### Admin endpoints

Use the admin secret header:

```http
X-Admin-Secret: <secret>
```

### Important

- Do **not** use `X-Api-Key` for normal object API access.
- Permission middleware accepts reader/writer style permissions and compatible read/write names.

---

## Documentation

| Document | Description |
|----------|-------------|
| [docs/API.md](docs/API.md) | HTTP API reference |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Storage internals and topology |
| [docs/DISTRIBUTED_ARCHITECTURE.md](docs/DISTRIBUTED_ARCHITECTURE.md) | Multi-node direction |
| [docs/INTEGRATION.md](docs/INTEGRATION.md) | Backend integration patterns |
| [docs/VISION.md](docs/VISION.md) | Canonical architecture/product intent |

---

## Development

```bash
make test
make build
```

For production-like usage, run the built binary via systemd rather than ad-hoc shell sessions.

---

## License

MIT. See [LICENSE](LICENSE).
