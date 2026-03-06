# oelala-storage Documentation

> **Distributed storage network for AI-generated content**

## 📚 Documentation Index

| Document | Description |
|----------|-------------|
| [DISTRIBUTED_ARCHITECTURE.md](DISTRIBUTED_ARCHITECTURE.md) | **Start here** - Vision for coordinator + storage nodes |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Current single-node technical architecture |
| [API.md](API.md) | HTTP API reference (S3-compatible) |
| [INTEGRATION.md](INTEGRATION.md) | Integration with oelala backend |

---

## 🎯 What is oelala-storage?

A Go-based distributed storage service that enables:

1. **User-operated storage nodes** - Run on Windows/Linux
2. **Automatic replication** - 2x/3x redundancy across nodes
3. **Cloudflare CDN** - Global access via tunnels
4. **Coordinator** - Central management in oelala backend

```
┌─────────────────────────────────────────────────────────────────┐
│                          COORDINATOR                             │
│                      (oelala main server)                        │
│  - Node registry   - File placement   - Cloudflare DNS          │
└─────────────────────────────────────────────────────────────────┘
                              │
         ┌────────────────────┼────────────────────┐
         ▼                    ▼                    ▼
┌──────────────────┐ ┌──────────────────┐ ┌──────────────────┐
│   STORAGE NODE   │ │   STORAGE NODE   │ │   STORAGE NODE   │
│   (Server)       │ │   (Windows PC)   │ │   (Linux box)    │
└──────────────────┘ └──────────────────┘ └──────────────────┘
```

## 🚀 Quick Start (Node)

```bash
# Download
./oelala-storage

# Setup wizard
./oelala-storage setup
# → Storage path: /mnt/data
# → Coordinator: https://api.oelala.xyz
# → API Key: oel_node_xxxx

# Install as service
./oelala-storage install

# Start
./oelala-storage serve
```

## 📊 Ports

| Port | Service |
|------|---------|
| 7990 | HTTP API |
| 7991 | gRPC (sync/replication) |
| 7992 | Prometheus metrics |

## 🔧 Configuration

```yaml
# oelala-storage.yaml
node:
  name: "my-node"
  storage_path: "/mnt/data"
  max_storage_gb: 500

coordinator:
  url: "https://api.oelala.xyz"
  api_key: "oel_node_xxxxxxxxxxxx"

server:
  http_port: 7990
  grpc_port: 7991
```

## 📦 Features

### Current (v1)
- ✅ S3-compatible API (PUT/GET/DELETE/HEAD)
- ✅ Content-addressed storage (SHA-256)
- ✅ Quota tracking per user
- ✅ Usage metering
- ✅ Prometheus metrics
- ✅ TLS support

### Planned (v2 - Distributed)
- 🔲 Coordinator registration
- 🔲 Windows Service installer
- 🔲 Setup wizard CLI
- 🔲 Node-to-node replication
- 🔲 Cloudflare tunnel integration
- 🔲 Automatic failover

## 🖥️ Platform Support

| Platform | Status |
|----------|--------|
| Linux | ✅ Supported |
| Windows | ✅ Supported |
| macOS | ❌ Not supported |
| iOS | ❌ Not supported |

## 🔗 Related

- [oelala](https://github.com/m0nklabs/oelala) - Main platform
- [AGENT_CONTEXT.md](../AGENT_CONTEXT.md) - Development context
