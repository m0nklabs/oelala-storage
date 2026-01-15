### Added
- **Garbage Collection (#2)**
  - Automatic GC runs hourly to delete expired objects
  - `X-Expires-At` header support on upload (RFC3339 format)
  - `GET /gc/stats` - View GC statistics (files deleted, bytes freed, etc.)
  - `POST /gc/run` - Trigger manual GC run via API
  - Background GC with configurable interval (default 1 hour)
  - CLI `gc` command with `--dry-run` and `--stats` flags

- **Content-Addressed Deduplication (#4)**
  - Content-addressed blob storage with SHA-256 hashing
  - Reference counting for shared blobs
  - Automatic blob cleanup when last reference is deleted
  - `GET /dedup/stats` - View deduplication statistics
  - Upload to `dedup/` bucket or use `X-Dedup: true` header
  - Stats include: blobs, references, bytes saved, deduplication percentage
  - Blobs stored in sharded directories (first 2 chars of hash)

### Changed
- `putObject` now supports deduplication for `dedup/*` buckets
- `getObject` retrieves from dedup store for deduplicated files  
- `deleteObject` decrements ref count and cleans up orphan blobs
- Server struct expanded with `dedupStore` and `gcFunctions`
