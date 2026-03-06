package dedup

import (
	"encoding/json"
	"fmt"

	"github.com/dgraph-io/badger/v4"
)

// Move renames/moves a reference from source to destination
// It handles ref counts correctly if over-writing an existing file
func (s *Store) Move(srcBucket, srcKey, destBucket, destKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(txn *badger.Txn) error {
		// Get source reference
		srcRefItem, err := txn.Get(refKey(srcBucket, srcKey))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return fmt.Errorf("source not found")
			}
			return fmt.Errorf("failed to get source reference: %w", err)
		}

		var srcRef Reference
		if err := srcRefItem.Value(func(val []byte) error {
			return json.Unmarshal(val, &srcRef)
		}); err != nil {
			return fmt.Errorf("failed to unmarshal source reference: %w", err)
		}

		// Handle potential overwrite at destination
		destRefItem, err := txn.Get(refKey(destBucket, destKey))
		if err == nil {
			// Destination exists, need to handle its blob's ref count
			var destRef Reference
			if err := destRefItem.Value(func(val []byte) error {
				return json.Unmarshal(val, &destRef)
			}); err == nil && destRef.BlobHash != srcRef.BlobHash {
				// Different blob at destination, decrement its ref count
				if err := s.decrementRefCount(txn, destRef.BlobHash); err != nil {
					return fmt.Errorf("failed to decrement replaced ref count: %w", err)
				}
			}

			// If overwriting self (src == dest hash), the total references decreases by 1
			if err == nil && destRef.BlobHash == srcRef.BlobHash {
				if err := s.decrementRefCount(txn, destRef.BlobHash); err != nil {
					return fmt.Errorf("failed to decrement ref count for identical hash overwrite: %w", err)
				}
			}
		}

		// Create new reference for destination
		newRef := Reference{
			Bucket:    destBucket,
			Key:       destKey,
			BlobHash:  srcRef.BlobHash,
			CreatedAt: srcRef.CreatedAt, // Preserve creation time
		}

		newRefData, err := json.Marshal(newRef)
		if err != nil {
			return fmt.Errorf("failed to marshal new reference: %w", err)
		}

		// Save the new reference
		if err := txn.Set(refKey(destBucket, destKey), newRefData); err != nil {
			return fmt.Errorf("failed to save new reference: %w", err)
		}

		// Delete the old reference
		if err := txn.Delete(refKey(srcBucket, srcKey)); err != nil {
			return fmt.Errorf("failed to delete source reference: %w", err)
		}

		return nil
	})
}
