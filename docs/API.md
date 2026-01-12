# API Reference

Complete API reference for oelala-storage service.

## Base URL

```
http://localhost:7990
```

For production, use your configured domain and port.

## Authentication

All API endpoints (except `/health` and `/status`) require authentication.

### Bearer Token (Recommended)

Use JWT tokens from Supabase:

```http
Authorization: Bearer <jwt_token>
```

### API Key

For service-to-service communication:

```http
X-API-Key: <api_key>
```

Or:

```http
Authorization: ApiKey <api_key>
```

## Rate Limiting

Currently not implemented. Future versions will enforce rate limits based on subscription tier.

## Endpoints

### Health Check

Check service health status.

**Endpoint:** `GET /health`

**Authentication:** Not required

**Response:**

```json
{
  "status": "healthy",
  "service": "oelala-storage"
}
```

**Example:**

```bash
curl http://localhost:7990/health
```

```python
import httpx

async with httpx.AsyncClient() as client:
    response = await client.get("http://localhost:7990/health")
    print(response.json())
```

---

### Service Status

Get detailed service status.

**Endpoint:** `GET /status`

**Authentication:** Not required

**Response:**

```json
{
  "status": "running"
}
```

**Example:**

```bash
curl http://localhost:7990/status
```

---

### Upload Object

Upload a file to storage.

**Endpoint:** `PUT /:bucket/:key`

**Authentication:** Required

**Parameters:**
- `bucket` (path) - Bucket name (e.g., "users")
- `key` (path) - Object key/path (supports nested paths with `/`)

**Headers:**
- `Authorization` - Bearer token or API key
- `Content-Type` - MIME type of the file (optional, auto-detected)

**Request Body:** Binary file content

**Response:** `201 Created`

```json
{
  "bucket": "users",
  "key": "123/media/video.mp4",
  "hash": "sha256:a1b2c3d4...",
  "size": 1048576,
  "content_type": "video/mp4",
  "created_at": "2026-01-12T10:30:00Z"
}
```

**Example:**

```bash
# Upload a file
curl -X PUT \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: video/mp4" \
  --data-binary @video.mp4 \
  http://localhost:7990/users/123e4567/media/video.mp4
```

```python
import httpx
from pathlib import Path

async def upload_file(
    bucket: str,
    key: str,
    file_path: Path,
    jwt_token: str
):
    url = f"http://localhost:7990/{bucket}/{key}"
    headers = {"Authorization": f"Bearer {jwt_token}"}
    
    with open(file_path, "rb") as f:
        async with httpx.AsyncClient() as client:
            response = await client.put(
                url,
                headers=headers,
                content=f.read()
            )
            response.raise_for_status()
            return response.json()

# Usage
result = await upload_file(
    bucket="users",
    key="123e4567/media/video.mp4",
    file_path=Path("video.mp4"),
    jwt_token="your-jwt-token"
)
print(f"Uploaded: {result['hash']}")
```

**Error Responses:**

- `400 Bad Request` - Empty body
- `401 Unauthorized` - Invalid or missing authentication
- `402 Payment Required` - Quota exceeded
- `500 Internal Server Error` - Upload failed

---

### Download Object

Download a file from storage.

**Endpoint:** `GET /:bucket/:key`

**Authentication:** Required

**Parameters:**
- `bucket` (path) - Bucket name
- `key` (path) - Object key/path

**Response:** `200 OK` with file content

**Headers:**
- `Content-Type` - MIME type of the file
- `Content-Length` - File size in bytes

**Example:**

```bash
# Download a file
curl -H "Authorization: Bearer <token>" \
  http://localhost:7990/users/123e4567/media/video.mp4 \
  -o downloaded_video.mp4
```

```python
import httpx

async def download_file(
    bucket: str,
    key: str,
    jwt_token: str,
    output_path: Path
):
    url = f"http://localhost:7990/{bucket}/{key}"
    headers = {"Authorization": f"Bearer {jwt_token}"}
    
    async with httpx.AsyncClient() as client:
        async with client.stream("GET", url, headers=headers) as response:
            response.raise_for_status()
            
            with open(output_path, "wb") as f:
                async for chunk in response.aiter_bytes():
                    f.write(chunk)

# Usage
await download_file(
    bucket="users",
    key="123e4567/media/video.mp4",
    jwt_token="your-jwt-token",
    output_path=Path("downloaded.mp4")
)
```

**Error Responses:**

- `401 Unauthorized` - Invalid or missing authentication
- `404 Not Found` - Object does not exist

---

### Delete Object

Delete a file from storage.

**Endpoint:** `DELETE /:bucket/:key`

**Authentication:** Required

**Parameters:**
- `bucket` (path) - Bucket name
- `key` (path) - Object key/path

**Response:** `204 No Content`

**Example:**

```bash
curl -X DELETE \
  -H "Authorization: Bearer <token>" \
  http://localhost:7990/users/123e4567/media/old_video.mp4
```

```python
import httpx

async def delete_file(bucket: str, key: str, jwt_token: str):
    url = f"http://localhost:7990/{bucket}/{key}"
    headers = {"Authorization": f"Bearer {jwt_token}"}
    
    async with httpx.AsyncClient() as client:
        response = await client.delete(url, headers=headers)
        response.raise_for_status()

# Usage
await delete_file(
    bucket="users",
    key="123e4567/media/old_video.mp4",
    jwt_token="your-jwt-token"
)
```

**Error Responses:**

- `401 Unauthorized` - Invalid or missing authentication
- `404 Not Found` - Object does not exist

---

### Get Object Metadata

Get metadata for an object without downloading it.

**Endpoint:** `HEAD /:bucket/:key`

**Authentication:** Required

**Parameters:**
- `bucket` (path) - Bucket name
- `key` (path) - Object key/path

**Response:** `200 OK` with headers only (no body)

**Headers:**
- `Content-Length` - File size in bytes

**Example:**

```bash
curl -I \
  -H "Authorization: Bearer <token>" \
  http://localhost:7990/users/123e4567/media/video.mp4
```

```python
import httpx

async def get_metadata(bucket: str, key: str, jwt_token: str):
    url = f"http://localhost:7990/{bucket}/{key}"
    headers = {"Authorization": f"Bearer {jwt_token}"}
    
    async with httpx.AsyncClient() as client:
        response = await client.head(url, headers=headers)
        response.raise_for_status()
        
        return {
            "size": int(response.headers["Content-Length"]),
            "exists": True
        }

# Usage
metadata = await get_metadata(
    bucket="users",
    key="123e4567/media/video.mp4",
    jwt_token="your-jwt-token"
)
print(f"File size: {metadata['size']} bytes")
```

**Error Responses:**

- `401 Unauthorized` - Invalid or missing authentication
- `404 Not Found` - Object does not exist

---

### List Objects

List objects in a bucket with optional prefix filtering.

**Endpoint:** `GET /:bucket`

**Authentication:** Required

**Parameters:**
- `bucket` (path) - Bucket name
- `prefix` (query, optional) - Filter objects by prefix

**Response:** `200 OK`

```json
{
  "bucket": "users",
  "prefix": "123/media/",
  "objects": [
    {
      "bucket": "users",
      "key": "123/media/video1.mp4",
      "hash": "sha256:a1b2c3d4...",
      "size": 1048576,
      "content_type": "video/mp4",
      "created_at": "2026-01-12T10:30:00Z"
    },
    {
      "bucket": "users",
      "key": "123/media/image.png",
      "hash": "sha256:e5f6g7h8...",
      "size": 524288,
      "content_type": "image/png",
      "created_at": "2026-01-12T11:00:00Z"
    }
  ],
  "count": 2
}
```

**Example:**

```bash
# List all objects in bucket
curl -H "Authorization: Bearer <token>" \
  http://localhost:7990/users

# List objects with prefix
curl -H "Authorization: Bearer <token>" \
  "http://localhost:7990/users?prefix=123/media/videos"
```

```python
import httpx

async def list_objects(
    bucket: str,
    jwt_token: str,
    prefix: str = ""
):
    url = f"http://localhost:7990/{bucket}"
    headers = {"Authorization": f"Bearer {jwt_token}"}
    params = {"prefix": prefix} if prefix else {}
    
    async with httpx.AsyncClient() as client:
        response = await client.get(url, headers=headers, params=params)
        response.raise_for_status()
        return response.json()

# Usage
result = await list_objects(
    bucket="users",
    jwt_token="your-jwt-token",
    prefix="123e4567/media/"
)

print(f"Found {result['count']} objects:")
for obj in result["objects"]:
    print(f"  - {obj['key']} ({obj['size']} bytes)")
```

**Error Responses:**

- `401 Unauthorized` - Invalid or missing authentication
- `500 Internal Server Error` - List operation failed

---

### List Peers

List connected sync peers.

**Endpoint:** `GET /peers`

**Authentication:** Required

**Response:** `200 OK`

```json
{
  "peers": []
}
```

**Note:** Peer synchronization is currently in development. This endpoint returns an empty list.

---

### Add Peer

Add a new sync peer.

**Endpoint:** `POST /peers`

**Authentication:** Required

**Response:** `501 Not Implemented`

**Note:** This feature is planned for future release.

---

### Remove Peer

Remove a sync peer.

**Endpoint:** `DELETE /peers/:id`

**Authentication:** Required

**Response:** `501 Not Implemented`

**Note:** This feature is planned for future release.

---

## Complete Usage Example

Here's a complete example of using the API in a Python application:

```python
import httpx
from pathlib import Path
from typing import Optional, List
import asyncio

class OelalaStorage:
    """Client for oelala-storage API."""
    
    def __init__(self, base_url: str = "http://localhost:7990"):
        self.base_url = base_url
        self.client = httpx.AsyncClient(timeout=300.0)
    
    def set_token(self, jwt_token: str):
        """Set JWT token for authentication."""
        self.client.headers["Authorization"] = f"Bearer {jwt_token}"
    
    async def upload(
        self,
        bucket: str,
        key: str,
        file_path: Path,
        content_type: Optional[str] = None
    ) -> dict:
        """Upload a file to storage."""
        url = f"{self.base_url}/{bucket}/{key}"
        
        headers = {}
        if content_type:
            headers["Content-Type"] = content_type
        
        with open(file_path, "rb") as f:
            response = await self.client.put(
                url,
                headers=headers,
                content=f.read()
            )
        
        response.raise_for_status()
        return response.json()
    
    async def download(
        self,
        bucket: str,
        key: str,
        output_path: Path
    ):
        """Download a file from storage."""
        url = f"{self.base_url}/{bucket}/{key}"
        
        async with self.client.stream("GET", url) as response:
            response.raise_for_status()
            
            with open(output_path, "wb") as f:
                async for chunk in response.aiter_bytes():
                    f.write(chunk)
    
    async def delete(self, bucket: str, key: str):
        """Delete a file from storage."""
        url = f"{self.base_url}/{bucket}/{key}"
        response = await self.client.delete(url)
        response.raise_for_status()
    
    async def exists(self, bucket: str, key: str) -> bool:
        """Check if a file exists."""
        url = f"{self.base_url}/{bucket}/{key}"
        try:
            response = await self.client.head(url)
            return response.status_code == 200
        except httpx.HTTPStatusError:
            return False
    
    async def get_size(self, bucket: str, key: str) -> int:
        """Get file size in bytes."""
        url = f"{self.base_url}/{bucket}/{key}"
        response = await self.client.head(url)
        response.raise_for_status()
        return int(response.headers["Content-Length"])
    
    async def list(
        self,
        bucket: str,
        prefix: str = ""
    ) -> List[dict]:
        """List objects in a bucket."""
        url = f"{self.base_url}/{bucket}"
        params = {"prefix": prefix} if prefix else {}
        
        response = await self.client.get(url, params=params)
        response.raise_for_status()
        
        data = response.json()
        return data["objects"]
    
    async def health(self) -> dict:
        """Check service health."""
        url = f"{self.base_url}/health"
        response = await self.client.get(url)
        response.raise_for_status()
        return response.json()
    
    async def close(self):
        """Close the HTTP client."""
        await self.client.aclose()


async def main():
    """Example usage of the storage client."""
    storage = OelalaStorage()
    storage.set_token("your-jwt-token-here")
    
    try:
        # Check health
        health = await storage.health()
        print(f"Service status: {health['status']}")
        
        # Upload a file
        result = await storage.upload(
            bucket="users",
            key="123e4567/media/test.txt",
            file_path=Path("test.txt"),
            content_type="text/plain"
        )
        print(f"Uploaded: {result['key']}, hash: {result['hash']}")
        
        # Check if file exists
        exists = await storage.exists("users", "123e4567/media/test.txt")
        print(f"File exists: {exists}")
        
        # Get file size
        size = await storage.get_size("users", "123e4567/media/test.txt")
        print(f"File size: {size} bytes")
        
        # List files
        objects = await storage.list("users", prefix="123e4567/media/")
        print(f"Found {len(objects)} files:")
        for obj in objects:
            print(f"  - {obj['key']} ({obj['size']} bytes)")
        
        # Download the file
        await storage.download(
            bucket="users",
            key="123e4567/media/test.txt",
            output_path=Path("downloaded.txt")
        )
        print("Downloaded successfully")
        
        # Delete the file
        await storage.delete("users", "123e4567/media/test.txt")
        print("Deleted successfully")
        
    finally:
        await storage.close()


if __name__ == "__main__":
    asyncio.run(main())
```

## Error Handling

### Error Response Format

All errors follow this format:

```json
{
  "error": "error_message"
}
```

### HTTP Status Codes

| Code | Meaning | Description |
|------|---------|-------------|
| 200 | OK | Request successful |
| 201 | Created | Upload successful |
| 204 | No Content | Delete successful |
| 400 | Bad Request | Invalid request (e.g., empty body) |
| 401 | Unauthorized | Invalid or missing authentication |
| 402 | Payment Required | Quota exceeded |
| 403 | Forbidden | Insufficient permissions |
| 404 | Not Found | Object does not exist |
| 500 | Internal Server Error | Server error |
| 501 | Not Implemented | Feature not yet implemented |
| 503 | Service Unavailable | Service temporarily unavailable |

### Common Error Scenarios

#### Quota Exceeded

```json
{
  "error": "storage_quota_exceeded",
  "used": 2147483648,
  "limit": 2147483648,
  "upgrade_url": "https://oelala.ai/upgrade"
}
```

#### Invalid Token

```json
{
  "error": "unauthorized"
}
```

#### Object Not Found

```json
{
  "error": "object not found: users/123/missing.mp4"
}
```

## Content Type Detection

The service automatically detects content types based on:

1. **File extension** (primary method)
2. **Magic bytes** (file header inspection)
3. **Content-Type header** (if provided)

Supported content types include:

| Extension | Content Type |
|-----------|-------------|
| .mp4 | video/mp4 |
| .webm | video/webm |
| .avi | video/x-msvideo |
| .mov | video/quicktime |
| .mp3 | audio/mpeg |
| .wav | audio/wav |
| .ogg | audio/ogg |
| .png | image/png |
| .jpg, .jpeg | image/jpeg |
| .gif | image/gif |
| .webp | image/webp |
| .txt | text/plain |
| .json | application/json |
| .pdf | application/pdf |

## Request Size Limits

- **Maximum upload size:** 100 GB
- **Timeout:** 5 minutes for large uploads
- **Recommended chunk size:** For files > 100 MB, consider implementing chunked uploads

## Best Practices

1. **Always check health** before starting bulk operations
2. **Use appropriate timeouts** for large file uploads/downloads
3. **Implement retry logic** for transient network failures
4. **Stream large files** instead of loading into memory
5. **Validate file sizes** before upload to avoid quota errors
6. **Use prefixes** for efficient listing of related objects
7. **Clean up** temporary files after successful upload
8. **Log all operations** for audit and debugging
9. **Handle errors gracefully** with user-friendly messages
10. **Close clients** properly to avoid connection leaks

## Rate Limits (Future)

Rate limiting will be implemented based on subscription tiers:

| Tier | Requests/min | Bandwidth |
|------|--------------|-----------|
| Free | 60 | 10 GB/month |
| Creator | 300 | 100 GB/month |
| Pro | 1000 | 500 GB/month |
| Studio | Unlimited | 2 TB/month |

## Versioning

The API follows semantic versioning. Current version: **v1.0.0**

Breaking changes will be announced with major version updates.

## Support

For issues or questions:
- GitHub Issues: https://github.com/m0nklabs/oelala-storage/issues
- Documentation: https://github.com/m0nklabs/oelala-storage/tree/main/docs

## See Also

- [INTEGRATION.md](./INTEGRATION.md) - Backend integration guide
- [ARCHITECTURE.md](./ARCHITECTURE.md) - System architecture
