package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

// Move renames/moves a file from source to destination
func (s *Store) Move(srcBucket, srcKey, destBucket, destKey string) error {
	srcPath := s.FilePath(srcBucket, srcKey)
	destPath := s.FilePath(destBucket, destKey)

	// Ensure destination bucket/directory exists
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	err := os.Rename(srcPath, destPath)
	if err != nil {
		return fmt.Errorf("failed to move file: %w", err)
	}

	return nil
}
