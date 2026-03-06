# System Architecture

> Last Updated: 2026-03-06

This document describes the current architecture of `oelala-storage` as implemented in the Go codebase.

## Overview

`oelala-storage` is a media-oriented object storage service with three active concerns:

1. **Object API** for upload, read, delete, list, move, and signed access
2. **State layer** for metadata, deduplication, retention, quotas, and metering
3. **Distributed groundwork** for heartbeats, discovery, gRPC sync, and peer replication

The design rule remains simple:

> The application decides policy. Storage executes object operations reliably.

## High-Level Topology

```text
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
│  • bucket + admin endpoints                                 │
├──────────────────────────────────────────────────────────────┤
│ State + Storage                                             │
│  • filesystem object store                                  │
│  • Badger metadata store                                    │
│  • content-addressed dedup references                       │
│  • quota + metering                                         │
│  • retention / GC                                           │
│  • webhook dispatch                                         │
├──────────────────────────────────────────────────────────────┤
│ Distributed Layer                                           │
│  • coordinator heartbeat client                             │
│  • peer discovery                                           │
│  • gRPC sync server                                         │
│  • replication / pull / push                                │
└──────────────────────────────────────────────────────────────┘
```

## Module Map

| Package | Responsibility |
|---------|----------------|
| `internal/api` | HTTP routing and handlers |
| `internal/auth` | bearer auth, permission checks, signed URL validation |
| `internal/storage` | filesystem object store, move support, content-type helpers |
| `internal/metadata` | Badger-backed object metadata, expiry listing, GC state |
| `internal/dedup` | content-addressed blob/reference tracking |
| `internal/bucket` | bucket/account usage and quota info |
| `internal/quota` | tier/quota enforcement logic |
| `internal/metering` | upload/download/delete/generation event recording |
| `internal/webhook` | async webhook dispatch with signing and retry |
| `internal/coordinator` | heartbeat client and node reporting |
| `internal/sync` | discovery, gRPC sync server, replication logic |
| `internal/signedurl` | signed access helpers |
| `internal/tls` | TLS loading / self-signed generation support |
| `internal/metrics` | Prometheus metrics exposure |
| `internal/apikeys` | API key persistence and validation helpers |
| `internal/cmd` | CLI commands such as `serve`, `gc`, and stats |

## HTTP/API Layer

**Technology**: Fiber

### Main Responsibilities

- upload/download/delete/head/list objects
- move/rename objects via `POST ...?action=move`
- apply auth and permission checks
- expose bucket and admin/status endpoints
- support Range requests, content-disposition, cache headers, and signed URL access

### Object Operations

- `PUT /:bucket/*` upload
- `GET /:bucket/*` download with Range support
- `HEAD /:bucket/*` metadata lookup
- `DELETE /:bucket/*` delete
- `GET /:bucket?list=true` listing with pagination semantics
- `POST /:bucket/*?action=move` move/rename objects

## Auth Model

### Object API

Normal application access uses:

```http
Authorization: Bearer <token>
```

### Admin API

Administrative surfaces use:

```http
X-Admin-Secret: <secret>
```

### Other Supported Access Forms

- signed URLs for time-limited read access
- permission aliases compatible with `read/write` and `reader/writer`

`X-Api-Key` should not be presented as the canonical modern object API example.

## State Layer

### Filesystem Object Store

`internal/storage` manages physical object bytes on disk, bucket layout, content-type detection, and object moves.

### Metadata Store

`internal/metadata` stores object metadata in Badger and supports:

- bucket/key lookup
- user-scoped listings
- storage usage lookup
- expired-object scans
- GC execution

### Deduplication

`internal/dedup` tracks content-addressed blobs and logical references so duplicate payloads do not need duplicate physical storage.

### Buckets / Quotas / Metering

The service already contains infrastructure for:

- bucket/account records
- quota checks and usage updates
- persistent metering for upload, download, delete, and generation-style events

These systems are designed to support upstream applications without embedding product-specific business logic in storage itself.

## Retention and GC

Retention is driven by upstream services through `X-Expires-At` metadata.

Storage responsibilities:

- persist expiration metadata
- expose GC stats/status
- delete expired objects when collected

## Webhooks

Webhook notifications are emitted asynchronously for events such as:

- `file.uploaded`
- `file.deleted`
- `quota.warning`
- `quota.exceeded`
- `file.expiring`
- `gc.completed`

Delivery includes:

- worker-based async dispatch
- retry/backoff
- HMAC-SHA256 signing
- verification helpers

## Distributed Layer

The codebase is already beyond a pure single-node toy design.

### Active Pieces

- coordinator client with heartbeat payloads
- node config with `public_url`
- peer discovery
- gRPC sync server
- replication engine with push/pull behavior
- static peer support

### Current Real-World Shape

| Node | Role | Notes |
|------|------|-------|
| storage-main | coordinator / primary | main public entrypoint |
| storage-node-01 | local extra node | separate local service + ports |
| storage-node-02 | remote node | independent host and tunnel |

## Observability and Operations

The service exposes operational surfaces for:

- Prometheus metrics
- request logging and recovery
- CLI `gc` and stats commands
- heartbeat/status reporting
- admin-facing node and bucket visibility

## Platform Policy Notes

- Supported platforms: **Windows and Linux only**
- product policy, subscriptions, and user entitlements stay upstream
- storage should execute reliably, not become the application brain