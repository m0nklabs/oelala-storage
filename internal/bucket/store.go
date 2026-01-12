// Package bucket provides user bucket management for storage quotas and user isolation.
package bucket

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/m0nklabs/oelala-storage/internal/quota"
)

// Info contains bucket information for a user
type Info struct {
	UserID     string    `json:"user_id"`
	Tier       string    `json:"tier"`
	QuotaBytes int64     `json:"quota_bytes"`
	UsedBytes  int64     `json:"used_bytes"`
	FileCount  int64     `json:"file_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// CreateRequest is the payload for creating a bucket
type CreateRequest struct {
	UserID  string `json:"user_id"`
	Tier    string `json:"tier"`
	QuotaGB int64  `json:"quota_gb,omitempty"` // Override tier default if set
}

// UpdateRequest is the payload for updating a bucket
type UpdateRequest struct {
	Tier    string `json:"tier,omitempty"`
	QuotaGB int64  `json:"quota_gb,omitempty"`
}

// Store manages bucket information
type Store struct {
	db *badger.DB
	mu sync.RWMutex
}

// NewStore creates a bucket store using an existing BadgerDB instance
func NewStore(db *badger.DB) *Store {
	return &Store{db: db}
}

func bucketKey(userID string) []byte {
	return []byte(fmt.Sprintf("bucket:%s", userID))
}

// Create creates a new user bucket
func (s *Store) Create(req *CreateRequest) (*Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if bucket already exists
	existing, _ := s.get(req.UserID)
	if existing != nil {
		return nil, fmt.Errorf("bucket already exists for user %s", req.UserID)
	}

	// Get tier defaults
	tier := quota.GetTier(req.Tier)
	quotaBytes := tier.QuotaBytes
	if req.QuotaGB > 0 {
		quotaBytes = req.QuotaGB * 1024 * 1024 * 1024
	}

	now := time.Now()
	info := &Info{
		UserID:     req.UserID,
		Tier:       req.Tier,
		QuotaBytes: quotaBytes,
		UsedBytes:  0,
		FileCount:  0,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.put(info); err != nil {
		return nil, err
	}

	return info, nil
}

// Get retrieves bucket info for a user
func (s *Store) Get(userID string) (*Info, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.get(userID)
}

func (s *Store) get(userID string) (*Info, error) {
	var info Info
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(bucketKey(userID))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &info)
		})
	})
	if err != nil {
		return nil, fmt.Errorf("bucket not found for user %s", userID)
	}
	return &info, nil
}

func (s *Store) put(info *Info) error {
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal bucket info: %w", err)
	}
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set(bucketKey(info.UserID), data)
	})
}

// Update updates bucket tier or quota
func (s *Store) Update(userID string, req *UpdateRequest) (*Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, err := s.get(userID)
	if err != nil {
		return nil, err
	}

	if req.Tier != "" {
		info.Tier = req.Tier
		tier := quota.GetTier(req.Tier)
		info.QuotaBytes = tier.QuotaBytes
	}

	if req.QuotaGB > 0 {
		info.QuotaBytes = req.QuotaGB * 1024 * 1024 * 1024
	}

	info.UpdatedAt = time.Now()

	if err := s.put(info); err != nil {
		return nil, err
	}

	return info, nil
}

// AddUsage adds bytes and file count to bucket usage
func (s *Store) AddUsage(userID string, bytes int64, files int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, err := s.get(userID)
	if err != nil {
		// Auto-create bucket with free tier if not exists
		info = &Info{
			UserID:     userID,
			Tier:       "free",
			QuotaBytes: quota.FreeTier.QuotaBytes,
			UsedBytes:  0,
			FileCount:  0,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
	}

	info.UsedBytes += bytes
	info.FileCount += files
	info.UpdatedAt = time.Now()

	// Don't go negative
	if info.UsedBytes < 0 {
		info.UsedBytes = 0
	}
	if info.FileCount < 0 {
		info.FileCount = 0
	}

	return s.put(info)
}

// CheckQuota returns true if user has quota available
func (s *Store) CheckQuota(userID string, additionalBytes int64) (bool, *Info, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, err := s.get(userID)
	if err != nil {
		// No bucket = free tier default
		info = &Info{
			UserID:     userID,
			Tier:       "free",
			QuotaBytes: quota.FreeTier.QuotaBytes,
			UsedBytes:  0,
		}
	}

	available := info.QuotaBytes - info.UsedBytes
	return additionalBytes <= available, info, nil
}

// Delete removes a bucket (admin only)
func (s *Store) Delete(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(bucketKey(userID))
	})
}

// List returns all buckets (admin only, paginated)
func (s *Store) List(limit int, offset int) ([]*Info, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var buckets []*Info
	count := 0
	skipped := 0

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("bucket:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			if skipped < offset {
				skipped++
				continue
			}
			if limit > 0 && count >= limit {
				break
			}

			item := it.Item()
			var info Info
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &info)
			})
			if err != nil {
				continue
			}
			buckets = append(buckets, &info)
			count++
		}
		return nil
	})

	return buckets, err
}
