### Added
- Webhook notification system for storage events (`internal/webhook/`)
- Event types: `file.uploaded`, `file.deleted`, `quota.warning`, `quota.exceeded`, `file.expiring`, `gc.completed`
- HMAC-SHA256 request signing with `X-Webhook-Signature` header
- Async delivery with configurable retry and exponential backoff
- Per-target event filtering
- `Verify()` helper function for webhook consumers
- Integration into `putObject` and `deleteObject` API handlers
- Webhook configuration section in `oelala-storage.yaml.example`
