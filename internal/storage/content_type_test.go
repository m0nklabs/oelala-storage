package storage

import (
	"bytes"
	"strings"
	"testing"
)

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		filename string
		expected string
	}{
		{
			name:     "JPEG from magic bytes",
			data:     []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46},
			filename: "",
			expected: "image/jpeg",
		},
		{
			name:     "PNG from magic bytes",
			data:     []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
			filename: "",
			expected: "image/png",
		},
		{
			name:     "GIF from magic bytes",
			data:     []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61},
			filename: "",
			expected: "image/gif",
		},
		{
			name:     "PDF from magic bytes",
			data:     []byte{0x25, 0x50, 0x44, 0x46, 0x2D},
			filename: "",
			expected: "application/pdf",
		},
		{
			name:     "MP4 from extension",
			data:     []byte{0x00, 0x00, 0x00},
			filename: "video.mp4",
			expected: "video/mp4",
		},
		{
			name:     "WebM from extension",
			data:     []byte{0x00, 0x00},
			filename: "video.webm",
			expected: "video/webm",
		},
		{
			name:     "FLAC from extension",
			data:     []byte{0x00},
			filename: "audio.flac",
			expected: "audio/flac",
		},
		{
			name:     "SafeTensors from extension",
			data:     []byte{0x00},
			filename: "model.safetensors",
			expected: "application/octet-stream",
		},
		{
			name:     "Extension case insensitive",
			data:     []byte{0x00},
			filename: "VIDEO.MP4",
			expected: "video/mp4",
		},
		{
			name:     "Unknown type",
			data:     []byte{0x00, 0x01, 0x02, 0x03},
			filename: "unknown.xyz",
			expected: "application/octet-stream",
		},
		{
			name:     "Empty data with extension",
			data:     []byte{},
			filename: "image.png",
			expected: "image/png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectContentType(tt.data, tt.filename)
			if result != tt.expected {
				t.Errorf("DetectContentType() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestDetectFromReader(t *testing.T) {
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	pngData := make([]byte, len(pngHeader)+100)
	copy(pngData, pngHeader)

	contentType, buf, err := DetectFromReader(bytes.NewReader(pngData), "")
	if err != nil {
		t.Fatalf("DetectFromReader() error = %v", err)
	}
	if contentType != "image/png" {
		t.Errorf("DetectFromReader() contentType = %q, want %q", contentType, "image/png")
	}
	if !bytes.HasPrefix(buf, pngHeader) {
		t.Errorf("DetectFromReader() buf doesn't have PNG header")
	}
}

func TestDetectFromReaderSmallFile(t *testing.T) {
	smallData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

	contentType, buf, err := DetectFromReader(bytes.NewReader(smallData), "test.png")
	if err != nil {
		t.Fatalf("DetectFromReader() error = %v", err)
	}
	if contentType != "image/png" {
		t.Errorf("DetectFromReader() contentType = %q, want %q", contentType, "image/png")
	}
	if len(buf) != len(smallData) {
		t.Errorf("DetectFromReader() buf length = %d, want %d", len(buf), len(smallData))
	}
}

func TestIsMediaType(t *testing.T) {
	tests := []struct {
		contentType string
		expected    bool
	}{
		{"video/mp4", true},
		{"video/webm", true},
		{"audio/mpeg", true},
		{"audio/wav", true},
		{"image/jpeg", true},
		{"image/png", true},
		{"application/json", false},
		{"application/octet-stream", false},
		{"text/plain", false},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			if got := IsMediaType(tt.contentType); got != tt.expected {
				t.Errorf("IsMediaType(%q) = %v, want %v", tt.contentType, got, tt.expected)
			}
		})
	}
}

func TestIsVideoType(t *testing.T) {
	if !IsVideoType("video/mp4") {
		t.Error("IsVideoType(video/mp4) should be true")
	}
	if IsVideoType("audio/mp3") {
		t.Error("IsVideoType(audio/mp3) should be false")
	}
}

func TestIsAudioType(t *testing.T) {
	if !IsAudioType("audio/mpeg") {
		t.Error("IsAudioType(audio/mpeg) should be true")
	}
	if IsAudioType("video/mp4") {
		t.Error("IsAudioType(video/mp4) should be false")
	}
}

func TestIsImageType(t *testing.T) {
	if !IsImageType("image/png") {
		t.Error("IsImageType(image/png) should be true")
	}
	if IsImageType("video/mp4") {
		t.Error("IsImageType(video/mp4) should be false")
	}
}

func TestContentCategory(t *testing.T) {
	tests := []struct {
		contentType string
		expected    string
	}{
		{"video/mp4", "video"},
		{"video/webm", "video"},
		{"audio/mpeg", "audio"},
		{"audio/wav", "audio"},
		{"image/jpeg", "image"},
		{"image/png", "image"},
		{"application/json", "other"},
		{"application/octet-stream", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			if got := ContentCategory(tt.contentType); got != tt.expected {
				t.Errorf("ContentCategory(%q) = %q, want %q", tt.contentType, got, tt.expected)
			}
		})
	}
}

func TestExtensionTypes(t *testing.T) {
	expectedExtensions := []string{
		".mp4", ".webm", ".mov", ".mkv",
		".mp3", ".wav", ".flac", ".ogg",
		".jpg", ".jpeg", ".png", ".gif", ".webp",
		".json", ".pdf", ".zip",
		".safetensors", ".ckpt", ".pt",
	}

	for _, ext := range expectedExtensions {
		if _, ok := extensionTypes[ext]; !ok {
			t.Errorf("extensionTypes missing %q", ext)
		}
	}
}

func TestDetectContentTypePrioritizesMagicBytes(t *testing.T) {
	pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	result := DetectContentType(pngData, "misleading.jpg")
	if !strings.HasPrefix(result, "image/png") {
		t.Errorf("Magic bytes should take priority: got %q, want image/png", result)
	}
}
