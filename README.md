# oelala-storage

**Distributed Storage Network for AI-Generated Content**

> Fast, scalable, self-hosted storage with multi-node replication.

---

## 🎯 What is oelala-storage?

oelala-storage is a **standalone distributed storage system** designed for:

- **AI/ML workloads** - Optimized for large media files (videos, images)
- **Self-hosted deployments** - Run on your own hardware
- **Multi-node replication** - 2x/3x redundancy across nodes
- **Simple integration** - REST API + gRPC for any application

While originally built for [oelala.ai](https://oelala.ai), it's designed as a **general-purpose storage product** that can be used by any project.

---

## 🚀 Quick Start

### Single Node (Development)

```bash
# Download
curl -LO https://github.com/m0nklabs/oelala-storage/releases/latest/download/oelala-storage-linux-amd64
chmod +x oelala-storage-linux-amd64

# Run
./oelala-storage-linux-amd64 serve --data-dir ./data
```

### Docker

```bash
docker run -d \
  -p 7990:7990 \
  -v /path/to/data:/data \
  ghcr.io/m0nklabs/oelala-storage:latest
```

### Configuration

```yaml
# oelala-storage.yaml
server:
  http_port: 7990
  grpc_port: 7991
  metrics_port: 7992

storage:
  data_dir: /data
  max_size_gb: 500

auth:
  admin_key: "your-admin-key"  # For Web UI access
```

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    oelala-storage Server                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │  REST API   │  │  gRPC API   │  │  Web UI (Admin)         │  │
│  │  :7990      │  │  :7991      │  │  :7990/admin            │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘  │
│         │               │                    │                  │
│         └───────────────┴────────────────────┘                  │
│                         │                                       │
│  ┌──────────────────────┴──────────────────────────────────────┐│
│  │                    Core Services                            ││
│  │  • Storage Engine (BadgerDB metadata + filesystem)          ││
│  │  • Bucket Management (user isolation, quotas)               ││
│  │  • API Key Auth (per-bucket or admin keys)                  ││
│  │  • Metrics (Prometheus)                                     ││
│  └─────────────────────────────────────────────────────────────┘│
│                                                                 │
├─────────────────────────────────────────────────────────────────┤
│  DISTRIBUTED MODE (optional)                                    │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │ Coordinator │  │ Replicator  │  │  Node Discovery         │  │
│  │             │  │             │  │                         │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

---

## 📚 Documentation

| Document | Description |
|----------|-------------|
| [Product Vision](docs/PRODUCT_VISION.md) | Why oelala-storage exists |
| [API Reference](docs/API.md) | REST & gRPC API documentation |
| [Architecture](docs/ARCHITECTURE.md) | System design overview |
| [Distributed Mode](docs/DISTRIBUTED_ARCHITECTURE.md) | Multi-node setup |
| [Integration Guide](docs/INTEGRATION.md) | Integrating with your app |

---

## 🔌 API Overview

### Upload a file

```bash
curl -X PUT "http://localhost:7990/files/my-bucket/video.mp4" \
  -H "X-API-Key: your-api-key" \
  -H "Content-Type: video/mp4" \
  --data-binary @video.mp4
```

### Download a file

```bash
curl "http://localhost:7990/files/my-bucket/video.mp4" \
  -H "X-API-Key: your-api-key" \
  -o video.mp4
```

### List files

```bash
curl "http://localhost:7990/files/my-bucket/?list=true" \
  -H "X-API-Key: your-api-key"
```

---

## 🔐 Authentication

oelala-storage supports multiple authentication modes:

| Mode | Use Case |
|------|----------|
| **Admin Key** | Full access, Web UI, management |
| **Bucket API Keys** | Per-bucket access for applications |
| **Service Keys** | Internal service-to-service auth |

---

## 📊 Monitoring

Prometheus metrics exposed on `:7992/metrics`:

- `storage_bytes_total` - Total storage used
- `storage_files_total` - Number of files
- `storage_requests_total` - Request count by operation
- `storage_request_duration_seconds` - Request latency

---

## 🛠️ Development

```bash
# Build
make build

# Test
make test

# Run locally
make run
```

---

## 📄 License

MIT License - See [LICENSE](LICENSE) for details.

---

## 🤝 Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

---

**Built with ❤️ for the AI generation community**
