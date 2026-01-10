bestudeer # Agent Context Transfer - oelala-storage

> **Mind meld document** - Alle context voor de volgende agent sessie.
> Laatst bijgewerkt: 2026-01-04

---

## 🎯 Project Missie

**oelala-storage** is een Go-based distributed storage service voor het **oelala** project - een AI video/audio content creation platform.

### Waarom bestaat dit project?
- **oelala** (Python/React) genereert grote media files (video, audio, images)
- Python is niet ideaal voor I/O-heavy storage daemons
- We willen local-first distributed storage (geen cloud lock-in)
- Single binary deployment op Windows, Linux, Android

---

## 🏗️ Architectuur Beslissingen

### Taal: Go (niet Python)
| Criterium | Go | Python |
|-----------|-----|--------|
| Single binary | ✅ | ❌ venv hell |
| I/O performance | ✅ goroutines | ❌ GIL |
| Cross-platform | ✅ cross-compile | ⚠️ dependencies |
| Memory footprint | ✅ ~10MB | ❌ ~100MB+ |

### Platform Support Policy
| Platform | Status |
|----------|--------|
| Windows | ✅ Supported |
| Linux | ✅ Supported |
| Android | ✅ Supported |
| macOS | ❌ **NIET ONDERSTEUND** |
| iOS | ❌ **NIET ONDERSTEUND** |

> **BELANGRIJK**: Apple platforms worden NOOIT ondersteund. Niet voorstellen, niet implementeren, niet documenteren.

### Storage Architectuur
```
┌─────────────────────────────────────────────────────────┐
│                    oelala (Python)                       │
│                   FastAPI Backend                        │
│                     Port 7998                            │
└─────────────────────┬───────────────────────────────────┘
                      │ HTTP (S3-compatible)
                      ▼
┌─────────────────────────────────────────────────────────┐
│              oelala-storage (Go)                         │
│                  HTTP API: 7990                          │
│                  gRPC Sync: 7991 (TODO)                  │
├─────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐      │
│  │   BadgerDB  │  │  File Store │  │  P2P Sync   │      │
│  │  (metadata) │  │ (SHA-256)   │  │  (gRPC)     │      │
│  └─────────────┘  └─────────────┘  └─────────────┘      │
└─────────────────────────────────────────────────────────┘
```

---

## 📁 Huidige Code Structuur

```
oelala-storage/
├── cmd/
│   └── oelala-storage/
│       └── main.go          # Entry point, version injection
├── internal/
│   ├── api/
│   │   └── server.go        # Fiber HTTP server, S3-compatible routes
│   ├── cmd/
│   │   └── root.go          # Cobra CLI (init, serve, version)
│   ├── config/
│   │   └── config.go        # Viper config loading (YAML)
│   └── storage/
│       └── store.go         # File storage, SHA-256 hashing
├── go.mod                   # Module: github.com/m0nklabs/oelala-storage
├── go.sum
├── Makefile                 # Build targets: linux, windows, android
├── README.md
└── .gitignore
```

---

## 🔧 Tech Stack

### Dependencies (go.mod)
```go
require (
    github.com/dgraph-io/badger/v4  // Metadata store (TODO)
    github.com/gofiber/fiber/v2     // HTTP framework
    github.com/spf13/cobra          // CLI framework
    github.com/spf13/viper          // Config management
    go.uber.org/zap                 // Structured logging (TODO)
    google.golang.org/grpc          // P2P sync (TODO)
)
```

### Ports
| Service | Port | Status |
|---------|------|--------|
| HTTP API | 7990 | ✅ Werkend |
| gRPC Sync | 7991 | ❌ TODO |

---

## ✅ Wat is AF

1. **CLI Framework** - Cobra met `init`, `serve`, `version` commands
2. **Config Loading** - Viper met YAML support
3. **File Storage** - Put/Get/Delete/List met SHA-256 content addressing
4. **HTTP API** - Fiber server met S3-compatible routes:
   - `PUT /:bucket/:key` - Upload
   - `GET /:bucket/:key` - Download
   - `DELETE /:bucket/:key` - Delete
   - `HEAD /:bucket/:key` - Metadata
   - `GET /:bucket?prefix=` - List
   - `GET /health` - Health check
5. **Graceful Shutdown** - Signal handling (SIGINT/SIGTERM)
6. **Cross-platform Makefile** - linux-amd64, windows-amd64, android-arm64

---

## ❌ Wat moet nog

### Prioriteit 1 (Core) - Foundation ✅ COMPLEET
- [x] **Unit tests** - store.go (78.6%), server.go (75%) ✅ Done 2026-01-04
- [x] **BadgerDB metadata store** - 84.9% coverage ✅ Done 2026-01-04
- [x] **Auth middleware** - Token verificatie, tier support, 96.8% coverage ✅ Done 2026-01-04
- [x] **Content-type detection** - Magic bytes + extension fallback, 83.2% coverage ✅ Done 2026-01-04

### Prioriteit 2 (Sync) - Multi-device ✅ COMPLEET
- [x] **gRPC proto definitions** - storage.proto met SyncService ✅ Done 2026-01-04
- [x] **Peer discovery** - mDNS discovery in internal/sync/discovery.go ✅ Done 2026-01-04
- [x] **Replication engine** - Push/pull sync in internal/sync/replicator.go ✅ Done 2026-01-04

### Prioriteit 3 (Production) - Stability ✅ COMPLEET
- [x] **Structured logging** - Zap in internal/logging/ ✅ Done 2026-01-04
- [x] **Metrics** - Prometheus in internal/metrics/ ✅ Done 2026-01-04
- [x] **TLS support** - Self-signed cert gen in internal/tls/ ✅ Done 2026-01-04
- [x] **Server integration** - All packages wired into cmd/root.go ✅ Done 2026-01-04
- [ ] **Android build testing** - Termux compatibility (manual test needed)

### Prioriteit 4 (CI/CD) - Development Infrastructure ✅ COMPLEET
- [x] **GitHub Actions CI** - Automated testing, linting, builds (#13) ✅ Done 2026-01-10
- [x] **GitHub Actions Release** - Automated releases with binaries (#14) ✅ Done 2026-01-10
- [x] **Linting configuration** - golangci-lint setup ✅ Done 2026-01-10
- [x] **Contributing guidelines** - Development workflow docs ✅ Done 2026-01-10

### Prioriteit 5 (Monetization-Ready) 💰 ✅ CORE COMPLEET
- [x] **Storage quota tracking** - internal/quota/tracker.go ✅ Done 2026-01-04
- [x] **Usage metering** - internal/metering/meter.go ✅ Done 2026-01-04
- [ ] **Retention policies** - 30 days free, unlimited paid
- [ ] **Signed URLs** - Tijdelijke shares, expiring links
- [ ] **Watermark injection** - Free tier watermark on output
- [ ] **Content deduplication** - Verlaagt storage costs

#### Storage Limits per Tier (uit MONETIZATION.md):
```
Free:     2 GB   (30 day retention)
Creator:  50 GB  (unlimited retention)
Pro:      200 GB (unlimited retention)
Studio:   1 TB   (unlimited retention)
```

#### API Changes voor Monetization:
```go
// User context in elke request
type StorageContext struct {
    UserID      string
    TierID      string // free, creator, pro, studio
    QuotaBytes  int64
    UsedBytes   int64
    Permissions []string // read, write, delete, admin
}

// Quota check middleware
func (s *Server) quotaMiddleware(c *fiber.Ctx) error {
    ctx := getStorageContext(c)
    if ctx.UsedBytes >= ctx.QuotaBytes {
        return c.Status(402).JSON(fiber.Map{
            "error": "storage_quota_exceeded",
            "used": ctx.UsedBytes,
            "limit": ctx.QuotaBytes,
            "upgrade_url": "https://oelala.ai/upgrade",
        })
    }
    return c.Next()
}
```

#### Metering Events (voor billing):
```go
type UsageEvent struct {
    UserID    string    `json:"user_id"`
    EventType string    `json:"event_type"` // upload, download, delete, generate
    Bytes     int64     `json:"bytes"`
    Timestamp time.Time `json:"timestamp"`
    Metadata  map[string]string `json:"metadata"`
}

// Event types:
// - storage.upload    (bytes stored)
// - storage.download  (bandwidth used)
// - storage.delete    (bytes freed)
// - generation.image  (1 credit)
// - generation.video  (5-20 credits based on duration)
// - generation.audio  (2 credits)
```

---

## 🔗 Relatie met oelala (main project)

### oelala project structuur
```
/home/flip/oelala/
├── src/
│   ├── backend/        # FastAPI (Python) - port 7998
│   └── frontend/       # React/Vite - port 5174
├── docs/
│   ├── MEDIA_STORAGE.md    # Storage architectuur
│   ├── MONETIZATION.md     # Business plan
│   └── ROADMAP.md          # Product roadmap
└── .github/
    └── copilot-instructions.md  # Agent instructies
```

### Integratie punten
1. **oelala backend** roept **oelala-storage** aan via HTTP
2. **Generated media** (video/audio/images) gaat naar storage
3. **User uploads** gaan via storage API
4. **Sync** repliceert tussen user's devices

---

## 👤 User Context

- **Team**: One-man operation (flip/m0nk111)
- **Philosophy**: Build it right from the start
- **Storage per node**: 500GB target
- **Priorities**: Performance > Scalability > Control > Speed > Dependencies > Cross-platform

---

## 🗣️ Communicatie Stijl

- **Taal met user**: Nederlands
- **Code/docs/commits**: Engels
- **Humor**: Toegestaan, beetje beidehand mag
- **Verbosity**: Kort en bondig, geen gelul

---

## 📋 Copilot Instructions Highlights

Uit `/home/flip/oelala/.github/copilot-instructions.md`:

```markdown
## Platform Support Policy
- Windows, Linux, Android: ✅ Supported
- macOS/iOS: ❌ NIET ONDERSTEUND - nooit voorstellen

## Related Repositories
- oelala-storage: Separate Go-based storage service

## Python / GPU Virtual Environments
- Canonical GPU venv: /home/flip/venvs/gpu

## Ports
- Frontend dev: 5174
- Backend API: 7998
- Storage HTTP: 7990
```

---

## 🧪 Test Commands

```bash
# Build
cd /home/flip/oelala-storage
export PATH=$PATH:/usr/local/go/bin
go build -o bin/oelala-storage ./cmd/oelala-storage

# Init config
./bin/oelala-storage init

# Start server
./bin/oelala-storage serve

# Test upload
curl -X PUT http://localhost:7990/test/hello.txt -d "Hello World"

# Test download
curl http://localhost:7990/test/hello.txt

# Test list
curl http://localhost:7990/test
```

---

## 🔮 Toekomstige Richting (Monetization-Aligned)

### Phase 1 (Current) - Local Storage ✅
Single node, S3-compatible API, file-based storage
- **Monetization**: None yet, dev/testing only

### Phase 2 - Multi-tenant Foundation 💰
User isolation, quota tracking, usage metering
- **Monetization**: Enables tiered pricing, quota upsells
- **Key features**: 
  - User namespacing (`/users/{user_id}/...`)
  - Storage quota enforcement
  - Usage event logging for billing

### Phase 3 - Multi-device Sync
gRPC sync protocol, peer discovery, conflict resolution
- **Monetization**: Premium feature (Pro/Studio tiers only)
- **Key features**:
  - Edge node sync
  - Selective folder sync
  - Offline-first for creators on-the-go

### Phase 4 - Retention & Cleanup
Automatic file expiry, tier-based retention
- **Monetization**: Free tier cleanup (30 days), paid = forever
- **Key features**:
  - TTL-based deletion
  - Warning emails before deletion
  - "Upgrade to keep" prompts

### Phase 5 - Optional Cloud Backend
S3/R2 as storage tier, CDN for delivery
- **Monetization**: Reduces infrastructure costs, enables global delivery
- **Key features**:
  - Hot/warm/cold tiering
  - CDN integration (CloudFlare)
  - Geographic redundancy for Studio tier

---

## 📊 Storage ↔ Monetization Matrix

| Feature | Free | Creator ($19) | Pro ($49) | Studio ($199) |
|---------|------|---------------|-----------|---------------|
| Storage quota | 2 GB | 50 GB | 200 GB | 1 TB |
| Retention | 30 days | ∞ | ∞ | ∞ |
| Multi-device sync | ❌ | ❌ | ✅ | ✅ |
| Edge nodes | ❌ | ❌ | 2 | ∞ |
| Signed URL shares | ❌ | 10/mo | 100/mo | ∞ |
| Bandwidth | 10 GB/mo | 100 GB/mo | 500 GB/mo | 2 TB/mo |
| Watermark | ✅ | ❌ | ❌ | ❌ |
| API access | ❌ | Limited | Full | Full |

---

## 📚 Relevante Bestanden om te Lezen

Als je meer context nodig hebt:

1. `/home/flip/oelala/docs/MEDIA_STORAGE.md` - Storage architectuur details
2. `/home/flip/oelala/docs/MONETIZATION.md` - Business model
3. `/home/flip/oelala/docs/ROADMAP.md` - Product roadmap
4. `/home/flip/oelala/.github/copilot-instructions.md` - Alle agent regels

---

## 🖖 Live Long and Prosper

Dit document bevat alle essentiële context voor het oelala-storage project.
Mocht er iets ontbreken: vraag de user, of check de docs in oelala repo.

*"The needs of the many outweigh the needs of the few."* - Maar in dit geval is het een one-man show, dus de needs zijn vrij aligned.
