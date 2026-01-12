// Package apikeys provides API key management with BadgerDB persistence.
package apikeys

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
)

var (
	ErrKeyNotFound = errors.New("api key not found")
	ErrKeyExists   = errors.New("api key already exists")
)

// APIKey represents a stored API key
type APIKey struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	KeyHash     string    `json:"key_hash"`   // SHA-256 hash of the actual key
	KeyPrefix   string    `json:"key_prefix"` // First 8 chars for display (osk_xxxx...)
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
	LastUsedAt  time.Time `json:"last_used_at,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	IsActive    bool      `json:"is_active"`
}

// Store manages API keys in BadgerDB
type Store struct {
	db *badger.DB
}

// NewStore creates a new API key store
func NewStore(dbPath string) (*Store, error) {
	opts := badger.DefaultOptions(dbPath).
		WithLogger(nil).
		WithLoggingLevel(badger.ERROR)

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open apikeys db: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database
func (s *Store) Close() error {
	return s.db.Close()
}

// generateKey creates a new random API key with prefix
func generateKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	// Format: osk_<64 hex chars> (osk = oelala storage key)
	return "osk_" + hex.EncodeToString(bytes), nil
}

// hashKey creates SHA-256 hash of the key
func hashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

// Create creates a new API key and returns the plaintext key (only shown once!)
func (s *Store) Create(name string, permissions []string) (plainKey string, key *APIKey, err error) {
	plainKey, err = generateKey()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate key: %w", err)
	}

	key = &APIKey{
		ID:          fmt.Sprintf("key_%d", time.Now().UnixNano()),
		Name:        name,
		KeyHash:     hashKey(plainKey),
		KeyPrefix:   plainKey[:12] + "...", // "osk_xxxxxxxx..."
		Permissions: permissions,
		CreatedAt:   time.Now(),
		IsActive:    true,
	}

	data, err := json.Marshal(key)
	if err != nil {
		return "", nil, err
	}

	err = s.db.Update(func(txn *badger.Txn) error {
		// Store by hash for lookup
		if err := txn.Set([]byte("key:hash:"+key.KeyHash), data); err != nil {
			return err
		}
		// Store by ID for listing
		return txn.Set([]byte("key:id:"+key.ID), data)
	})

	if err != nil {
		return "", nil, err
	}

	return plainKey, key, nil
}

// ValidateKey checks if a key is valid and returns the key info
func (s *Store) ValidateKey(plainKey string) (*APIKey, error) {
	hash := hashKey(plainKey)

	var key APIKey
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("key:hash:" + hash))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return ErrKeyNotFound
			}
			return err
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &key)
		})
	})

	if err != nil {
		return nil, err
	}

	if !key.IsActive {
		return nil, ErrKeyNotFound
	}

	if !key.ExpiresAt.IsZero() && time.Now().After(key.ExpiresAt) {
		return nil, ErrKeyNotFound
	}

	// Update last used (async, don't block)
	go s.updateLastUsed(key.ID)

	return &key, nil
}

// updateLastUsed updates the last used timestamp
func (s *Store) updateLastUsed(keyID string) {
	_ = s.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("key:id:" + keyID))
		if err != nil {
			return err
		}

		var key APIKey
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &key)
		})
		if err != nil {
			return err
		}

		key.LastUsedAt = time.Now()
		data, err := json.Marshal(key)
		if err != nil {
			return err
		}

		if err := txn.Set([]byte("key:id:"+keyID), data); err != nil {
			return err
		}
		return txn.Set([]byte("key:hash:"+key.KeyHash), data)
	})
}

// List returns all API keys (without the hash for security)
func (s *Store) List() ([]*APIKey, error) {
	var keys []*APIKey

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("key:id:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var key APIKey
				if err := json.Unmarshal(val, &key); err != nil {
					return err
				}
				// Don't expose the hash
				key.KeyHash = ""
				keys = append(keys, &key)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	return keys, err
}

// Revoke deactivates an API key
func (s *Store) Revoke(keyID string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("key:id:" + keyID))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return ErrKeyNotFound
			}
			return err
		}

		var key APIKey
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &key)
		})
		if err != nil {
			return err
		}

		key.IsActive = false
		data, err := json.Marshal(key)
		if err != nil {
			return err
		}

		if err := txn.Set([]byte("key:id:"+keyID), data); err != nil {
			return err
		}
		return txn.Set([]byte("key:hash:"+key.KeyHash), data)
	})
}

// Delete permanently removes an API key
func (s *Store) Delete(keyID string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		// Get key to find hash
		item, err := txn.Get([]byte("key:id:" + keyID))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return ErrKeyNotFound
			}
			return err
		}

		var key APIKey
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &key)
		})
		if err != nil {
			return err
		}

		// Delete both entries
		if err := txn.Delete([]byte("key:id:" + keyID)); err != nil {
			return err
		}
		return txn.Delete([]byte("key:hash:" + key.KeyHash))
	})
}

// HasPermission checks if a key has a specific permission
func (k *APIKey) HasPermission(perm string) bool {
	for _, p := range k.Permissions {
		if p == perm || p == "*" || p == "admin" {
			return true
		}
		// Wildcard matching: "read:*" matches "read:objects"
		if strings.HasSuffix(p, ":*") {
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(perm, prefix) {
				return true
			}
		}
	}
	return false
}
