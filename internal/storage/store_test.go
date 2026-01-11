package storage

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewStore(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("creates store with valid path", func(t *testing.T) {
		store, err := NewStore(tmpDir, 100)
		if err != nil {
			t.Fatalf("NewStore() error = %v", err)
		}
		if store == nil {
			t.Fatal("NewStore() returned nil")
		}
		if store.basePath != tmpDir {
			t.Errorf("basePath = %v, want %v", store.basePath, tmpDir)
		}
	})

	t.Run("creates nested directory", func(t *testing.T) {
		nestedPath := filepath.Join(tmpDir, "nested", "dir")
		store, err := NewStore(nestedPath, 50)
		if err != nil {
			t.Fatalf("NewStore() error = %v", err)
		}
		if store == nil {
			t.Fatal("NewStore() returned nil")
		}
		if _, err := os.Stat(nestedPath); os.IsNotExist(err) {
			t.Error("nested directory was not created")
		}
	})
}

func TestStorePut(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := NewStore(tmpDir, 100)

	t.Run("stores file successfully", func(t *testing.T) {
		content := "Hello, World!"
		reader := strings.NewReader(content)

		obj, err := store.Put("test-bucket", "hello.txt", reader)
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		if obj.Bucket != "test-bucket" {
			t.Errorf("Bucket = %v, want %v", obj.Bucket, "test-bucket")
		}
		if obj.Key != "hello.txt" {
			t.Errorf("Key = %v, want %v", obj.Key, "hello.txt")
		}
		if obj.Size != int64(len(content)) {
			t.Errorf("Size = %v, want %v", obj.Size, len(content))
		}
		if obj.Hash == "" {
			t.Error("Hash should not be empty")
		}

		filePath := filepath.Join(tmpDir, "test-bucket", "hello.txt")
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Error("file was not created on disk")
		}
	})

	t.Run("stores file with nested key", func(t *testing.T) {
		content := "Nested content"
		reader := strings.NewReader(content)

		obj, err := store.Put("test-bucket", "path/to/file.txt", reader)
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		if obj.Key != "path/to/file.txt" {
			t.Errorf("Key = %v, want %v", obj.Key, "path/to/file.txt")
		}

		filePath := filepath.Join(tmpDir, "test-bucket", "path", "to", "file.txt")
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Error("nested file was not created")
		}
	})

	t.Run("generates consistent hash for same content", func(t *testing.T) {
		content := "Test content for hashing"

		obj1, _ := store.Put("hash-bucket", "file1.txt", strings.NewReader(content))
		obj2, _ := store.Put("hash-bucket", "file2.txt", strings.NewReader(content))

		if obj1.Hash != obj2.Hash {
			t.Errorf("Hashes should match: %v != %v", obj1.Hash, obj2.Hash)
		}
	})

	t.Run("generates different hash for different content", func(t *testing.T) {
		obj1, _ := store.Put("hash-bucket", "diff1.txt", strings.NewReader("Content A"))
		obj2, _ := store.Put("hash-bucket", "diff2.txt", strings.NewReader("Content B"))

		if obj1.Hash == obj2.Hash {
			t.Error("Hashes should be different for different content")
		}
	})

	t.Run("detects content type from magic bytes", func(t *testing.T) {
		// PNG magic bytes
		pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
		pngData = append(pngData, []byte("fake png content")...)

		obj, err := store.Put("content-type-bucket", "image.png", bytes.NewReader(pngData))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		if obj.ContentType != "image/png" {
			t.Errorf("ContentType = %v, want image/png", obj.ContentType)
		}
	})

	t.Run("detects content type from extension fallback", func(t *testing.T) {
		// No magic bytes, just text
		obj, err := store.Put("content-type-bucket", "video.mp4", strings.NewReader("fake mp4"))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		if obj.ContentType != "video/mp4" {
			t.Errorf("ContentType = %v, want video/mp4", obj.ContentType)
		}
	})
}

func TestStoreGet(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := NewStore(tmpDir, 100)

	t.Run("retrieves stored file", func(t *testing.T) {
		content := "Content to retrieve"
		_, _ = store.Put("get-bucket", "retrieve.txt", strings.NewReader(content))

		reader, obj, err := store.Get("get-bucket", "retrieve.txt")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		defer func() { _ = reader.Close() }()

		if obj.Bucket != "get-bucket" {
			t.Errorf("Bucket = %v, want %v", obj.Bucket, "get-bucket")
		}
		if obj.Key != "retrieve.txt" {
			t.Errorf("Key = %v, want %v", obj.Key, "retrieve.txt")
		}
		if obj.Size != int64(len(content)) {
			t.Errorf("Size = %v, want %v", obj.Size, len(content))
		}

		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if string(data) != content {
			t.Errorf("Content = %v, want %v", string(data), content)
		}
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		_, _, err := store.Get("get-bucket", "nonexistent.txt")
		if err == nil {
			t.Error("Get() should return error for non-existent file")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("Error should contain 'not found': %v", err)
		}
	})

	t.Run("returns error for non-existent bucket", func(t *testing.T) {
		_, _, err := store.Get("nonexistent-bucket", "file.txt")
		if err == nil {
			t.Error("Get() should return error for non-existent bucket")
		}
	})
}

func TestStoreDelete(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := NewStore(tmpDir, 100)

	t.Run("deletes existing file", func(t *testing.T) {
		_, _ = store.Put("delete-bucket", "todelete.txt", strings.NewReader("Delete me"))

		err := store.Delete("delete-bucket", "todelete.txt")
		if err != nil {
			t.Fatalf("Delete() error = %v", err)
		}

		if store.Exists("delete-bucket", "todelete.txt") {
			t.Error("File should not exist after delete")
		}
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		err := store.Delete("delete-bucket", "nonexistent.txt")
		if err == nil {
			t.Error("Delete() should return error for non-existent file")
		}
	})
}

func TestStoreExists(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := NewStore(tmpDir, 100)

	t.Run("returns true for existing file", func(t *testing.T) {
		_, _ = store.Put("exists-bucket", "exists.txt", strings.NewReader("I exist"))

		if !store.Exists("exists-bucket", "exists.txt") {
			t.Error("Exists() should return true for existing file")
		}
	})

	t.Run("returns false for non-existent file", func(t *testing.T) {
		if store.Exists("exists-bucket", "nope.txt") {
			t.Error("Exists() should return false for non-existent file")
		}
	})

	t.Run("returns false for non-existent bucket", func(t *testing.T) {
		if store.Exists("nope-bucket", "file.txt") {
			t.Error("Exists() should return false for non-existent bucket")
		}
	})
}

func TestStoreList(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := NewStore(tmpDir, 100)

	_, _ = store.Put("list-bucket", "file1.txt", strings.NewReader("File 1"))
	_, _ = store.Put("list-bucket", "file2.txt", strings.NewReader("File 2"))
	_, _ = store.Put("list-bucket", "images/img1.png", strings.NewReader("Image 1"))
	_, _ = store.Put("list-bucket", "images/img2.png", strings.NewReader("Image 2"))
	_, _ = store.Put("list-bucket", "docs/readme.md", strings.NewReader("Readme"))

	t.Run("lists all files in bucket", func(t *testing.T) {
		objects, err := store.List("list-bucket", "")
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}

		if len(objects) != 5 {
			t.Errorf("Expected 5 objects, got %d", len(objects))
		}
	})

	t.Run("filters by prefix", func(t *testing.T) {
		objects, err := store.List("list-bucket", "images/")
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}

		if len(objects) != 2 {
			t.Errorf("Expected 2 objects with prefix 'images/', got %d", len(objects))
		}

		for _, obj := range objects {
			if !strings.HasPrefix(obj.Key, "images/") {
				t.Errorf("Object key %s doesn't have expected prefix", obj.Key)
			}
		}
	})

	t.Run("returns empty for non-existent prefix", func(t *testing.T) {
		objects, err := store.List("list-bucket", "nonexistent/")
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}

		if len(objects) != 0 {
			t.Errorf("Expected 0 objects, got %d", len(objects))
		}
	})
}

func TestCopyFile(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("copies file contents", func(t *testing.T) {
		srcPath := filepath.Join(tmpDir, "source.txt")
		dstPath := filepath.Join(tmpDir, "dest.txt")
		content := []byte("Copy this content")

		if err := os.WriteFile(srcPath, content, 0644); err != nil {
			t.Fatalf("Failed to create source file: %v", err)
		}

		if err := copyFile(srcPath, dstPath); err != nil {
			t.Fatalf("copyFile() error = %v", err)
		}

		data, err := os.ReadFile(dstPath)
		if err != nil {
			t.Fatalf("Failed to read dest file: %v", err)
		}

		if !bytes.Equal(data, content) {
			t.Errorf("Content mismatch: got %v, want %v", data, content)
		}
	})
}

func TestMatchPrefix(t *testing.T) {
	tests := []struct {
		path   string
		prefix string
		want   bool
	}{
		{"images/photo.jpg", "images/", true},
		{"images/photo.jpg", "images", true},
		{"images/photo.jpg", "docs/", false},
		{"file.txt", "", true},
		{"file.txt", "file", true},
		{"file.txt", "file.txt", true},
		{"file.txt", "file.txt.bak", false},
		{"short", "longprefix", false},
	}

	for _, tt := range tests {
		t.Run(tt.path+"_"+tt.prefix, func(t *testing.T) {
			if got := matchPrefix(tt.path, tt.prefix); got != tt.want {
				t.Errorf("matchPrefix(%q, %q) = %v, want %v", tt.path, tt.prefix, got, tt.want)
			}
		})
	}
}

func BenchmarkPut(b *testing.B) {
	tmpDir := b.TempDir()
	store, _ := NewStore(tmpDir, 100)
	content := strings.Repeat("x", 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := filepath.Join("bench", string(rune('a'+i%26))+".txt")
		_, _ = store.Put("bench-bucket", key, strings.NewReader(content))
	}
}

func BenchmarkGet(b *testing.B) {
	tmpDir := b.TempDir()
	store, _ := NewStore(tmpDir, 100)
	content := strings.Repeat("x", 1024)
	_, _ = store.Put("bench-bucket", "bench.txt", strings.NewReader(content))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader, _, _ := store.Get("bench-bucket", "bench.txt")
		if reader != nil {
			_, _ = io.Copy(io.Discard, reader)
			_ = reader.Close()
		}
	}
}
