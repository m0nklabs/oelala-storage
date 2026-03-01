### Added
- **Range requests (HTTP 206)**: `getObject` supports `bytes=start-end` and suffix ranges for video seeking in browser
- **ETag + If-None-Match**: Conditional GET returns 304 Not Modified when content unchanged
- **Cache-Control**: `public, max-age=86400` on all GET/HEAD responses
- **Last-Modified**: UTC timestamp header on all object responses
- **Accept-Ranges: bytes**: Advertised on GET and HEAD responses
- **Content-Disposition**: `inline` for media types, `attachment` for binaries, `?download=true` override
- **LIST pagination**: `max_keys` (default 1000, max 10000), `marker`-based pagination, `delimiter` support, `is_truncated` + `next_marker` response fields
- **Permission enforcement**: `requirePermission("reader"|"writer")` middleware on S3-compatible CRUD routes (backward-compatible: empty roles = full access)
- **Content-Type from metadata/mime**: HEAD and GET resolve Content-Type from metadata store or file extension
- **PersistentMeter**: BadgerDB-backed metering with 90-day TTL, automatic warmup from disk on restart
- **`storage.Store.FilePath()`**: New method for direct file path access (used by Range handler)

### Fixed
- Test role name `write` → `writer` + `reader` to match middleware expectations
- Permission middleware accepts both `read`/`reader` and `write`/`writer` forms
