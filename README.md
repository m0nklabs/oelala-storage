# oelala-storage

> **Distributed storage network for AI-generated content**

A Go-based distributed storage service for the [oelala](https://github.com/m0nklabs/oelala) platform. Enables user-operated storage nodes with automatic replication and Cloudflare CDN integration.

## 🎯 Vision

See [docs/DISTRIBUTED_ARCHITECTURE.md](docs/DISTRIBUTED_ARCHITECTURE.md) for the full architecture.

```
Coordinator (oelala) ←→ Storage Nodes (users) ←→ Cloudflare CDN
```

## 🚀 Quick Start

```bash
# Download latest release
curl -LO https://github.com/m0nklabs/oelala-storage/releases/latest/download/oelala-storage-linux-amd64

# Run setup wizard
./oelala-storage setup

# Install as service
./oelala-storage install

# Start
./oelala-storage serve
```

## 📦 Features

- **S3-compatible API** - PUT/GET/DELETE/HEAD operations
- **Content-addressed storage** - SHA-256 deduplication
- **Multi-platform** - Windows & Linux support
- **Service mode** - Run as Windows Service or systemd unit
- **Metrics** - Prometheus endpoint at `/metrics`

## 🔧 Configuration

```yaml
# oelala-storage.yaml
node:
  name: "my-node"
  storage_path: "/mnt/data"
  max_storage_gb: 500

coordinator:
  url: "https://api.oelala.ai"
  api_key: "oel_node_xxxxxxxxxxxx"

server:
  http_port: 7990
  grpc_port: 7991
```

## 📊 Ports

| Port | Service |
|------|---------|
| 7990 | HTTP API |
| 7991 | gRPC (sync/replication) |
| 7992 | Prometheus metrics |

## 🔗 Related

- [oelala](https://github.com/m0nklabs/oelala) - Main platform
- [DISTRIBUTED_ARCHITECTURE.md](docs/DISTRIBUTED_ARCHITECTURE.md) - Full architecture
- [AGENT_CONTEXT.md](AGENT_CONTEXT.md) - Development context

## 📜 License

MIT
