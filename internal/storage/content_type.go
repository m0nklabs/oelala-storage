package storage

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

// ContentType detection for uploaded files
// Uses magic bytes (sniffing) with file extension fallback

// Common MIME types for media files
var extensionTypes = map[string]string{
	// Video
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mov":  "video/quicktime",
	".avi":  "video/x-msvideo",
	".mkv":  "video/x-matroska",
	".m4v":  "video/x-m4v",
	".wmv":  "video/x-ms-wmv",
	".flv":  "video/x-flv",
	".ogv":  "video/ogg",
	".3gp":  "video/3gpp",

	// Audio
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".flac": "audio/flac",
	".aac":  "audio/aac",
	".m4a":  "audio/mp4",
	".wma":  "audio/x-ms-wma",
	".opus": "audio/opus",

	// Images
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".svg":  "image/svg+xml",
	".ico":  "image/x-icon",
	".tiff": "image/tiff",
	".tif":  "image/tiff",
	".heic": "image/heic",
	".heif": "image/heif",
	".avif": "image/avif",

	// Documents / Data
	".json": "application/json",
	".xml":  "application/xml",
	".pdf":  "application/pdf",
	".zip":  "application/zip",
	".tar":  "application/x-tar",
	".gz":   "application/gzip",

	// AI/ML specific
	".safetensors": "application/octet-stream",
	".ckpt":        "application/octet-stream",
	".pt":          "application/octet-stream",
	".pth":         "application/octet-stream",
	".onnx":        "application/octet-stream",
}

// DetectContentType determines MIME type from content and filename
// Priority: magic bytes detection > extension lookup > default
func DetectContentType(data []byte, filename string) string {
	// Try magic bytes first (first 512 bytes)
	if len(data) > 0 {
		contentType := http.DetectContentType(data)
		// http.DetectContentType returns application/octet-stream for unknown
		// Only trust it if it found something specific
		if contentType != "application/octet-stream" && contentType != "text/plain; charset=utf-8" {
			return contentType
		}
	}

	// Fall back to extension
	if filename != "" {
		ext := strings.ToLower(filepath.Ext(filename))
		if ct, ok := extensionTypes[ext]; ok {
			return ct
		}
	}

	return "application/octet-stream"
}

// DetectFromReader reads up to 512 bytes to detect content type
// Returns detected type and the bytes read (for prepending to further reads)
func DetectFromReader(r io.Reader, filename string) (string, []byte, error) {
	// Read first 512 bytes for detection
	buf := make([]byte, 512)
	n, err := io.ReadFull(r, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", nil, err
	}

	buf = buf[:n]
	contentType := DetectContentType(buf, filename)
	return contentType, buf, nil
}

// IsMediaType checks if content type is video/audio/image
func IsMediaType(contentType string) bool {
	return strings.HasPrefix(contentType, "video/") ||
		strings.HasPrefix(contentType, "audio/") ||
		strings.HasPrefix(contentType, "image/")
}

// IsVideoType checks if content type is video
func IsVideoType(contentType string) bool {
	return strings.HasPrefix(contentType, "video/")
}

// IsAudioType checks if content type is audio
func IsAudioType(contentType string) bool {
	return strings.HasPrefix(contentType, "audio/")
}

// IsImageType checks if content type is image
func IsImageType(contentType string) bool {
	return strings.HasPrefix(contentType, "image/")
}

// ContentCategory returns the media category
func ContentCategory(contentType string) string {
	switch {
	case IsVideoType(contentType):
		return "video"
	case IsAudioType(contentType):
		return "audio"
	case IsImageType(contentType):
		return "image"
	default:
		return "other"
	}
}
