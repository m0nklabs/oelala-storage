### Added

- **Web UI for API Key Management** (`/admin/`)
  - Create new API keys with customizable permissions (read, write, delete, admin)
  - List all API keys with metadata (name, prefix, permissions, created/last used)
  - Revoke keys (disable without deleting)
  - Delete keys permanently
  - Keys are shown only once on creation - stored as SHA-256 hashes
  - Protected by `security.admin_secret` config option

- **Dynamic API Key Storage** (BadgerDB)
  - API keys stored in `{storage_path}/apikeys/` BadgerDB database
  - Keys format: `osk_<64 hex chars>` (oelala storage key)
  - Automatic last-used tracking
  - Support for key expiration (optional)

### Changed

- Auth middleware now supports dynamic key validation via callback
- Static YAML tokens still work, but dynamic keys via admin UI are preferred
- Admin routes (`/admin/*`) are excluded from API key authentication

### Security

- Admin UI requires `X-Admin-Secret` header or `?secret=` query param
- API key hashes stored, not plaintext - keys shown once on creation only
- Revoked keys immediately stop working
