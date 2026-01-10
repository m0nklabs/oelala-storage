# oelala-storage

> Go-based distributed storage service for Oelala

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org)
[![CI](https://github.com/m0nklabs/oelala-storage/actions/workflows/ci.yml/badge.svg)](https://github.com/m0nklabs/oelala-storage/actions/workflows/ci.yml)
[![Release](https://github.com/m0nklabs/oelala-storage/actions/workflows/release.yml/badge.svg)](https://github.com/m0nklabs/oelala-storage/actions/workflows/release.yml)
[![Platforms](https://img.shields.io/badge/Platforms-Windows%20%7C%20Linux%20%7C%20Android-blue)](/)
[![License](https://img.shields.io/badge/License-MIT-green)](LICENSE)

## Overview

`oelala-storage` is a lightweight, distributed storage node that provides:

- **S3-compatible API** - Works with existing tools and SDKs
- **P2P Sync** - Nodes sync directly with each other via gRPC
- **Content Addressing** - SHA-256 deduplication
- **Single Binary** - No runtime dependencies (~15MB)
- **Cross-Platform** - Windows, Linux, Android

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│              oelala-storage node (Go binary)                │
├─────────────────────────────────────────────────────────────┤
│  ┌───────────────┐  ┌───────────────┐  ┌───────────────┐   │
│  │  BadgerDB     │  │  HTTP API     │  │  Sync Engine  │   │
│  │  (metadata)   │  │  (S3-compat)  │  │  (gRPC P2P)   │   │
│  └───────────────┘  └───────────────┘  └───────────────┘   │
│  ┌───────────────┐  ┌───────────────┐  ┌───────────────┐   │
│  │  Local FS     │  │  LRU Cache    │  │  WebSocket    │   │
│  │  (blobs)      │  │  (hot files)  │  │  (events)     │   │
│  └───────────────┘  └───────────────┘  └───────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

## Quick Start

### Download

```bash
# Linux (amd64)
curl -LO https://github.com/m0nklabs/oelala-storage/releases/latest/download/oelala-storage-linux-amd64
chmod +x oelala-storage-linux-amd64

# Windows (amd64)
curl -LO https://github.com/m0nklabs/oelala-storage/releases/latest/download/oelala-storage-windows-amd64.exe
```

### Run

```bash
# Initialize config
./oelala-storage init

# Start node
./oelala-storage serve

# Or with custom config
./oelala-storage serve --config /path/to/config.yaml
```

### Configuration

```yaml
# oelala-storage.yaml
node:
  id: "auto"  # Auto-generate or specify
  name: "My Storage Node"
  type: primary  # primary, replica, edge, archive

storage:
  path: "./data"
  max_size_gb: 500
  cache_size_mb: 2048

api:
  http_port: 7999
  grpc_port: 7998
  enable_tls: false

sync:
  enabled: true
  peers: []
  strategy: realtime  # realtime, scheduled, manual
  interval_minutes: 15

security:
  encryption_at_rest: false
  auth_tokens:
    - name: "default"
      token: "${OELALA_STORAGE_TOKEN}"
      permissions: ["read", "write", "delete"]
```

## API Endpoints

### S3-Compatible

| Method | Endpoint | Description |
|--------|----------|-------------|
| `PUT` | `/{bucket}/{key}` | Upload object |
| `GET` | `/{bucket}/{key}` | Download object |
| `DELETE` | `/{bucket}/{key}` | Delete object |
| `HEAD` | `/{bucket}/{key}` | Object metadata |
| `GET` | `/{bucket}?list` | List objects |

### Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | Health check |
| `GET` | `/status` | Node status |
| `GET` | `/peers` | List sync peers |
| `POST` | `/peers` | Add sync peer |
| `DELETE` | `/peers/{id}` | Remove peer |

## Building

### Requirements

- Go 1.22+
- Make (optional)

### Build

```bash
# All platforms
make build-all

# Linux only
make build-linux

# Windows only  
make build-windows

# Android (arm64)
make build-android
```

### Development

```bash
# Run locally
go run cmd/oelala-storage/main.go serve

# Run tests
go test ./...

# Run tests with coverage
make test-coverage

# Lint
golangci-lint run
# Or use make
make lint
```

## CI/CD

This project uses GitHub Actions for continuous integration and releases:

### Continuous Integration

On every push and pull request:
- **Tests**: Run full test suite with race detection
- **Lint**: Check code quality with golangci-lint  
- **Build**: Cross-compile for all supported platforms
- **Coverage**: Generate test coverage reports

### Releases

To create a new release:

1. Tag the commit with semantic versioning:
   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```

2. GitHub Actions will automatically:
   - Build binaries for all platforms
   - Generate a changelog from commits
   - Create a GitHub release
   - Upload binaries and checksums

Releases are available at: https://github.com/m0nklabs/oelala-storage/releases

## Platforms

| Platform | Architecture | Status |
|----------|--------------|--------|
| Linux | amd64, arm64 | ✅ |
| Windows | amd64 | ✅ |
| Android | arm64 | ✅ |
| macOS | - | ❌ Not supported |
| iOS | - | ❌ Not supported |

## Integration with Oelala

```python
# In oelala backend (FastAPI)
import boto3

# Connect to oelala-storage using S3 SDK
s3 = boto3.client(
    's3',
    endpoint_url='http://localhost:7999',
    aws_access_key_id='oelala',
    aws_secret_access_key='your-token'
)

# Upload
s3.upload_file('video.mp4', 'generated', 'user123/video.mp4')

# Download  
s3.download_file('generated', 'user123/video.mp4', 'local.mp4')
```

## Related Projects

- [oelala](https://github.com/m0nklabs/oelala) - Main application (video generation)

## License

MIT License - see [LICENSE](LICENSE)
