// Package metadata provides metadata storage and retrieval for objects using BadgerDB.
package metadata

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// ObjectMeta contains metadata about stored objects
type ObjectMeta struct {
	Key         string            `json:"key"`
	Bucket      string            `json:"bucket"`
	Size        int64             `json:"size"`
	ContentType string            `json:"content_type,omitempty"`
	Hash        string            `json:"hash"`
	UserID      string            `json:"user_id,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	ModifiedAt  time.Time         `json:"modified_at"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// Store handles metadata storage with BadgerDB
type Store struct {
	db *badger.DB
}

// Options for creating a new metadata store
type Options struct {
	Path       string
	InMemory   bool
	SyncWrites bool
	LogLevel   string
}

// DefaultOptions returns sensible defaults
func DefaultOptions(path string) Options {
	return Options{
		Path:       path,
		InMemory:   false,
		SyncWrites: false,
		LogLevel:   "warn",
	}
}

// NewStore creates a new metadata store backed by BadgerDB
func NewStore(opts Options) (*Store, error) {
	badgerOpts := badger.DefaultOptions(opts.Path)
	badgerOpts.Logger = nil

	if opts.InMemory {
		badgerOpts = badger.DefaultOptions("").WithInMemory(true)
	}

	badgerOpts.SyncWrites = opts.SyncWrites

	db, err := badger.Open(badgerOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to open metadata store: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the metadata store
func (s *Store) Close() error {
	return s.db.Close()
}

func objectKey(bucket, key string) []byte {
	return []byte(fmt.Sprintf("object:%s:%s", bucket, key))
}

func userObjectsPrefix(userID string) []byte {
	return []byte(fmt.Sprintf("user:%s:objects:", userID))
}

func userObjectKey(userID, bucket, key string) []byte {
	return []byte(fmt.Sprintf("user:%s:objects:%s:%s", userID, bucket, key))
}

// Put stores object metadata
func (s *Store) Put(meta *ObjectMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return s.db.Update(func(txn *badger.Txn) error {
		if err := txn.Set(objectKey(meta.Bucket, meta.Key), data); err != nil {
			return err
		}

		if meta.UserID != "" {
			if err := txn.Set(userObjectKey(meta.UserID, meta.Bucket, meta.Key), []byte{}); err != nil {
				return err
			}
		}

		return nil
	})
}

// Get retrieves object metadata
func (s *Store) Get(bucket, key string) (*ObjectMeta, error) {
	var meta ObjectMeta

	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(objectKey(bucket, key))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return fmt.Errorf("object not found: %s/%s", bucket, key)
			}
			return err
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &meta)
		})
	})

	if err != nil {
		return nil, err
	}
	return &meta, nil
}

// Delete removes object metadata
func (s *Store) Delete(bucket, key string) error {
	meta, err := s.Get(bucket, key)
	if err != nil {
		return err
	}

	return s.db.Update(func(txn *badger.Txn) error {
		if err := txn.Delete(objectKey(bucket, key)); err != nil {
			return err
		}

		if meta.UserID != "" {
			if err := txn.Delete(userObjectKey(meta.UserID, bucket, key)); err != nil {
				if err != badger.ErrKeyNotFound {
					return err
				}
			}
		}

		return nil
	})
}

// Exists checks if object metadata exists
func (s *Store) Exists(bucket, key string) bool {
	err := s.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get(objectKey(bucket, key))
		return err
	})
	return err == nil
}

// ListByBucket returns all objects in a bucket
func (s *Store) ListByBucket(bucket string) ([]*ObjectMeta, error) {
	var objects []*ObjectMeta
	prefix := []byte(fmt.Sprintf("object:%s:", bucket))

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var meta ObjectMeta
				if err := json.Unmarshal(val, &meta); err != nil {
					return err
				}
				objects = append(objects, &meta)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	return objects, err
}

// ListByUser returns all objects belonging to a user
func (s *Store) ListByUser(userID string) ([]*ObjectMeta, error) {
	var objects []*ObjectMeta
	prefix := userObjectsPrefix(userID)

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			keyStr := string(item.Key())
			parts := strings.SplitN(strings.TrimPrefix(keyStr, string(prefix)), ":", 2)
			if len(parts) != 2 {
				continue
			}
			bucket, key := parts[0], parts[1]

			objItem, err := txn.Get(objectKey(bucket, key))
			if err != nil {
				continue
			}

			err = objItem.Value(func(val []byte) error {
				var meta ObjectMeta
				if err := json.Unmarshal(val, &meta); err != nil {
					return err
				}
				objects = append(objects, &meta)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	return objects, err
}

// GetUserStorageUsed calculates total storage used by a user
func (s *Store) GetUserStorageUsed(userID string) (int64, error) {
	objects, err := s.ListByUser(userID)
	if err != nil {
		return 0, err
	}

	var total int64
	for _, obj := range objects {
		total += obj.Size
	}
	return total, nil
}

// ListExpired returns objects that have passed their expiration time
func (s *Store) ListExpired() ([]*ObjectMeta, error) {
	var expired []*ObjectMeta
	now := time.Now()

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte("object:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var meta ObjectMeta
				if err := json.Unmarshal(val, &meta); err != nil {
					return err
				}
				if meta.ExpiresAt != nil && meta.ExpiresAt.Before(now) {
					expired = append(expired, &meta)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	return expired, err
}

// RunGC runs garbage collection on the BadgerDB
func (s *Store) RunGC() error {
	return s.db.RunValueLogGC(0.5)
}
