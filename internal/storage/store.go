// Package storage provides object storage functionality with content-type detection and deduplication.
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Object represents a stored file
type Object struct {
	Key          string            `json:"key"`
	Bucket       string            `json:"bucket"`
	Size         int64             `json:"size"`
	ContentType  string            `json:"content_type"`
	Hash         string            `json:"hash"` // SHA-256
	CreatedAt    time.Time         `json:"created_at"`
	ModifiedAt   time.Time         `json:"modified_at"`
	UserMetadata map[string]string `json:"user_metadata,omitempty"`
}

// Store handles file storage operations
type Store struct {
	basePath string
	maxSize  int64 // bytes
}

// NewStore creates a new file store
func NewStore(basePath string, maxSizeGB int) (*Store, error) {
	// Ensure base path exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	return &Store{
		basePath: basePath,
		maxSize:  int64(maxSizeGB) * 1024 * 1024 * 1024,
	}, nil
}

// Put stores a file and returns its hash
func (s *Store) Put(bucket, key string, reader io.Reader) (*Object, error) {
	// Create bucket directory
	bucketPath := filepath.Join(s.basePath, bucket)
	if err := os.MkdirAll(bucketPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create bucket: %w", err)
	}

	// Detect content type from first 512 bytes
	contentType, headerBytes, err := DetectFromReader(reader, key)
	if err != nil {
		return nil, fmt.Errorf("failed to detect content type: %w", err)
	}

	// Create temp file for hashing
	tmpFile, err := os.CreateTemp(s.basePath, "upload-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	// Calculate hash while writing
	hasher := sha256.New()
	writer := io.MultiWriter(tmpFile, hasher)

	// Write header bytes first
	n, err := writer.Write(headerBytes)
	if err != nil {
		_ = tmpFile.Close()
		return nil, fmt.Errorf("failed to write header: %w", err)
	}
	size := int64(n)

	// Copy rest of reader
	copied, err := io.Copy(writer, reader)
	if err != nil {
		_ = tmpFile.Close()
		return nil, fmt.Errorf("failed to write file: %w", err)
	}
	size += copied
	_ = tmpFile.Close()

	hash := hex.EncodeToString(hasher.Sum(nil))

	// Move to final location
	finalPath := filepath.Join(bucketPath, key)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create key directory: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		// Rename failed (cross-device?), try copy
		if err := copyFile(tmpPath, finalPath); err != nil {
			return nil, fmt.Errorf("failed to move file: %w", err)
		}
	}

	now := time.Now()
	return &Object{
		Key:         key,
		Bucket:      bucket,
		Size:        size,
		ContentType: contentType,
		Hash:        hash,
		CreatedAt:   now,
		ModifiedAt:  now,
	}, nil
}

// Get retrieves a file
func (s *Store) Get(bucket, key string) (io.ReadCloser, *Object, error) {
	filePath := filepath.Join(s.basePath, bucket, key)

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("object not found: %s/%s", bucket, key)
		}
		return nil, nil, fmt.Errorf("failed to open file: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("failed to stat file: %w", err)
	}

	obj := &Object{
		Key:        key,
		Bucket:     bucket,
		Size:       stat.Size(),
		ModifiedAt: stat.ModTime(),
	}

	return file, obj, nil
}

// Delete removes a file
func (s *Store) Delete(bucket, key string) error {
	filePath := filepath.Join(s.basePath, bucket, key)
	return os.Remove(filePath)
}

// Exists checks if a file exists
func (s *Store) Exists(bucket, key string) bool {
	filePath := filepath.Join(s.basePath, bucket, key)
	_, err := os.Stat(filePath)
	return err == nil
}

// ListBuckets returns all top-level bucket names (directories in basePath)
func (s *Store) ListBuckets() ([]string, error) {
	entries, err := os.ReadDir(s.basePath)
	if err != nil {
		return nil, err
	}
	var buckets []string
	for _, e := range entries {
		if e.IsDir() {
			buckets = append(buckets, e.Name())
		}
	}
	return buckets, nil
}

// List returns all objects in a bucket with optional prefix
func (s *Store) List(bucket, prefix string) ([]*Object, error) {
	bucketPath := filepath.Join(s.basePath, bucket)
	var objects []*Object

	err := filepath.Walk(bucketPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(bucketPath, path)
		if err != nil {
			return err
		}

		// Filter by prefix
		if prefix != "" && !matchPrefix(rel, prefix) {
			return nil
		}

		objects = append(objects, &Object{
			Key:        rel,
			Bucket:     bucket,
			Size:       info.Size(),
			ModifiedAt: info.ModTime(),
		})
		return nil
	})

	return objects, err
}

func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()

	dest, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = dest.Close() }()

	_, err = io.Copy(dest, source)
	return err
}

// FilePath returns the absolute path to a stored file.
func (s *Store) FilePath(bucket, key string) string {
	return filepath.Join(s.basePath, bucket, key)
}

func matchPrefix(path, prefix string) bool {
	return len(path) >= len(prefix) && path[:len(prefix)] == prefix
}

// SetFileTime updates the physical modification time of the file
func (s *Store) SetFileTime(bucket, key string, mtime time.Time) error {
	filePath := filepath.Join(s.basePath, bucket, key)
	return os.Chtimes(filePath, mtime, mtime)
}
