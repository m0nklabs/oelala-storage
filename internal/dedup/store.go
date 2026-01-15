// Package dedup provides content-addressed storage with deduplication and reference counting.
package dedup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/m0nklabs/oelala-storage/internal/metrics"
)

// BlobInfo contains information about a content-addressed blob
type BlobInfo struct {
	Hash      string    `json:"hash"`       // SHA-256 hash of content
	Size      int64     `json:"size"`       // Size in bytes
	RefCount  int       `json:"ref_count"`  // Number of references to this blob
	CreatedAt time.Time `json:"created_at"` // When blob was first stored
}

// Reference links an object key to a blob hash
type Reference struct {
	Bucket    string    `json:"bucket"`
	Key       string    `json:"key"`
	BlobHash  string    `json:"blob_hash"`
	CreatedAt time.Time `json:"created_at"`
}

// Store provides content-addressed storage with deduplication
type Store struct {
	blobPath string       // Path to store blobs
	db       *badger.DB   // Metadata store
	mu       sync.RWMutex // Protects ref count operations
}

// Options for creating a new dedup store
type Options struct {
	BlobPath   string // Where to store blobs
	DBPath     string // Where to store BadgerDB metadata
	InMemory   bool   // Use in-memory storage (for testing)
	SyncWrites bool   // Sync writes to disk
}

// Stats contains deduplication statistics
type Stats struct {
	TotalBlobs       int     `json:"total_blobs"`
	TotalReferences  int     `json:"total_references"`
	TotalBytes       int64   `json:"total_bytes"`       // Actual bytes stored
	LogicalBytes     int64   `json:"logical_bytes"`     // Bytes if no dedup
	BytesSaved       int64   `json:"bytes_saved"`       // Savings from dedup
	DeduplicationPct float64 `json:"deduplication_pct"` // Percentage saved
}

// NewStore creates a new dedup store
func NewStore(opts Options) (*Store, error) {
	// Create blob directory
	if err := os.MkdirAll(opts.BlobPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create blob directory: %w", err)
	}

	// Setup BadgerDB
	badgerOpts := badger.DefaultOptions(opts.DBPath)
	badgerOpts.Logger = nil
	badgerOpts.SyncWrites = opts.SyncWrites

	if opts.InMemory {
		badgerOpts = badger.DefaultOptions("").WithInMemory(true)
	}

	db, err := badger.Open(badgerOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to open dedup metadata: %w", err)
	}

	return &Store{
		blobPath: opts.BlobPath,
		db:       db,
	}, nil
}

// Close closes the dedup store
func (s *Store) Close() error {
	return s.db.Close()
}

// blobKey returns the BadgerDB key for a blob
func blobKey(hash string) []byte {
	return []byte("blob:" + hash)
}

// refKey returns the BadgerDB key for a reference
func refKey(bucket, key string) []byte {
	return []byte(fmt.Sprintf("ref:%s:%s", bucket, key))
}

// blobPath returns the file path for a blob
func (s *Store) getBlobPath(hash string) string {
	// Use first 2 chars as subdirectory for better filesystem performance
	return filepath.Join(s.blobPath, hash[:2], hash)
}

// Store stores content and returns its hash, handling deduplication
func (s *Store) Store(bucket, key string, reader io.Reader) (string, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Write to temp file while computing hash
	tmpFile, err := os.CreateTemp(s.blobPath, "upload-*")
	if err != nil {
		return "", 0, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	hasher := sha256.New()
	writer := io.MultiWriter(tmpFile, hasher)

	size, err := io.Copy(writer, reader)
	if err != nil {
		_ = tmpFile.Close()
		return "", 0, fmt.Errorf("failed to write content: %w", err)
	}
	_ = tmpFile.Close()

	hash := hex.EncodeToString(hasher.Sum(nil))
	blobPath := s.getBlobPath(hash)

	// Check if blob already exists
	var blobInfo BlobInfo
	blobExists := false

	err = s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(blobKey(hash))
		if err == badger.ErrKeyNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			blobExists = true
			return json.Unmarshal(val, &blobInfo)
		})
	})
	if err != nil {
		return "", 0, fmt.Errorf("failed to check blob existence: %w", err)
	}

	now := time.Now()

	if blobExists {
		// Blob exists, just increment ref count
		blobInfo.RefCount++
		metrics.RecordDedupHit()
	} else {
		// New blob, move temp file to blob location
		metrics.RecordDedupMiss()
		if err := os.MkdirAll(filepath.Dir(blobPath), 0755); err != nil {
			return "", 0, fmt.Errorf("failed to create blob directory: %w", err)
		}

		if err := os.Rename(tmpPath, blobPath); err != nil {
			// Try copy if rename fails (cross-device)
			if err := copyFile(tmpPath, blobPath); err != nil {
				return "", 0, fmt.Errorf("failed to move blob: %w", err)
			}
		}

		blobInfo = BlobInfo{
			Hash:      hash,
			Size:      size,
			RefCount:  1,
			CreatedAt: now,
		}
	}

	// Create reference
	ref := Reference{
		Bucket:    bucket,
		Key:       key,
		BlobHash:  hash,
		CreatedAt: now,
	}

	// Update database
	err = s.db.Update(func(txn *badger.Txn) error {
		// Check if reference already exists (overwrite case)
		oldRefItem, err := txn.Get(refKey(bucket, key))
		if err == nil {
			// Old reference exists, need to decrement old blob's ref count
			var oldRef Reference
			err = oldRefItem.Value(func(val []byte) error {
				return json.Unmarshal(val, &oldRef)
			})
			if err == nil && oldRef.BlobHash != hash {
				// Different blob, decrement old ref count
				if err := s.decrementRefCount(txn, oldRef.BlobHash); err != nil {
					return err
				}
			} else if oldRef.BlobHash == hash {
				// Same blob, don't double-count
				blobInfo.RefCount--
			}
		}

		// Save blob info
		blobData, err := json.Marshal(blobInfo)
		if err != nil {
			return err
		}
		if err := txn.Set(blobKey(hash), blobData); err != nil {
			return err
		}

		// Save reference
		refData, err := json.Marshal(ref)
		if err != nil {
			return err
		}
		return txn.Set(refKey(bucket, key), refData)
	})

	if err != nil {
		return "", 0, fmt.Errorf("failed to update metadata: %w", err)
	}

	return hash, size, nil
}

// Get retrieves content by bucket/key
func (s *Store) Get(bucket, key string) (io.ReadCloser, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var ref Reference
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(refKey(bucket, key))
		if err == badger.ErrKeyNotFound {
			return fmt.Errorf("object not found: %s/%s", bucket, key)
		}
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &ref)
		})
	})
	if err != nil {
		return nil, "", err
	}

	blobPath := s.getBlobPath(ref.BlobHash)
	file, err := os.Open(blobPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open blob: %w", err)
	}

	return file, ref.BlobHash, nil
}

// Delete removes a reference and decrements blob ref count
func (s *Store) Delete(bucket, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(txn *badger.Txn) error {
		// Get reference
		item, err := txn.Get(refKey(bucket, key))
		if err == badger.ErrKeyNotFound {
			return nil // Already deleted
		}
		if err != nil {
			return err
		}

		var ref Reference
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &ref)
		}); err != nil {
			return err
		}

		// Delete reference
		if err := txn.Delete(refKey(bucket, key)); err != nil {
			return err
		}

		// Decrement blob ref count
		return s.decrementRefCount(txn, ref.BlobHash)
	})
}

// decrementRefCount decreases ref count and deletes blob if zero
func (s *Store) decrementRefCount(txn *badger.Txn, hash string) error {
	item, err := txn.Get(blobKey(hash))
	if err != nil {
		return err
	}

	var blobInfo BlobInfo
	if err := item.Value(func(val []byte) error {
		return json.Unmarshal(val, &blobInfo)
	}); err != nil {
		return err
	}

	blobInfo.RefCount--

	if blobInfo.RefCount <= 0 {
		// No more references, delete blob
		blobPath := s.getBlobPath(hash)
		if err := os.Remove(blobPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete blob file: %w", err)
		}
		return txn.Delete(blobKey(hash))
	}

	// Update ref count
	data, err := json.Marshal(blobInfo)
	if err != nil {
		return err
	}
	return txn.Set(blobKey(hash), data)
}

// GetBlobInfo returns information about a blob
func (s *Store) GetBlobInfo(hash string) (*BlobInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var info BlobInfo
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(blobKey(hash))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &info)
		})
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// GetReference returns the reference for a bucket/key
func (s *Store) GetReference(bucket, key string) (*Reference, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var ref Reference
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(refKey(bucket, key))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &ref)
		})
	})
	if err != nil {
		return nil, err
	}
	return &ref, nil
}

// Exists checks if a reference exists
func (s *Store) Exists(bucket, key string) bool {
	_, err := s.GetReference(bucket, key)
	return err == nil
}

// GetStats returns deduplication statistics
func (s *Store) GetStats() (*Stats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &Stats{}

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		it := txn.NewIterator(opts)
		defer it.Close()

		// Count blobs
		blobPrefix := []byte("blob:")
		for it.Seek(blobPrefix); it.ValidForPrefix(blobPrefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var info BlobInfo
				if err := json.Unmarshal(val, &info); err != nil {
					return err
				}
				stats.TotalBlobs++
				stats.TotalBytes += info.Size
				// Logical = what would be stored without dedup
				stats.LogicalBytes += info.Size * int64(info.RefCount)
				return nil
			})
			if err != nil {
				return err
			}
		}

		// Count references
		refPrefix := []byte("ref:")
		for it.Seek(refPrefix); it.ValidForPrefix(refPrefix); it.Next() {
			stats.TotalReferences++
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	stats.BytesSaved = stats.LogicalBytes - stats.TotalBytes
	if stats.LogicalBytes > 0 {
		stats.DeduplicationPct = float64(stats.BytesSaved) / float64(stats.LogicalBytes) * 100
	}

	return stats, nil
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
