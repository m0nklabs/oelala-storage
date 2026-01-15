package dedup

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestDedupStore(t *testing.T) {
	// Create temp directories
	tmpDir, err := os.MkdirTemp("", "dedup-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	blobDir := tmpDir + "/blobs"
	dbDir := tmpDir + "/db"

	store, err := NewStore(Options{
		BlobPath:   blobDir,
		DBPath:     dbDir,
		SyncWrites: false,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	content := []byte("Hello, this is test content for deduplication!")

	// Test 1: Store first copy
	hash1, size1, err := store.Store("bucket1", "file1.txt", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Failed to store first copy: %v", err)
	}
	if size1 != int64(len(content)) {
		t.Errorf("Size mismatch: got %d, want %d", size1, len(content))
	}

	// Test 2: Store duplicate (same content, different key)
	hash2, size2, err := store.Store("bucket1", "file2.txt", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Failed to store duplicate: %v", err)
	}

	// Hashes should match
	if hash1 != hash2 {
		t.Errorf("Hash mismatch: %s != %s", hash1, hash2)
	}
	if size1 != size2 {
		t.Errorf("Size mismatch: %d != %d", size1, size2)
	}

	// Test 3: Check blob info - should have refcount of 2
	blobInfo, err := store.GetBlobInfo(hash1)
	if err != nil {
		t.Fatalf("Failed to get blob info: %v", err)
	}
	if blobInfo.RefCount != 2 {
		t.Errorf("RefCount should be 2, got %d", blobInfo.RefCount)
	}

	// Test 4: Get content from both references
	reader1, _, err := store.Get("bucket1", "file1.txt")
	if err != nil {
		t.Fatalf("Failed to get file1: %v", err)
	}
	data1, _ := io.ReadAll(reader1)
	_ = reader1.Close()

	reader2, _, err := store.Get("bucket1", "file2.txt")
	if err != nil {
		t.Fatalf("Failed to get file2: %v", err)
	}
	data2, _ := io.ReadAll(reader2)
	_ = reader2.Close()

	if !bytes.Equal(data1, content) {
		t.Error("file1 content mismatch")
	}
	if !bytes.Equal(data2, content) {
		t.Error("file2 content mismatch")
	}

	// Test 5: Delete first reference
	if err := store.Delete("bucket1", "file1.txt"); err != nil {
		t.Fatalf("Failed to delete file1: %v", err)
	}

	// Blob should still exist with refcount 1
	blobInfo, err = store.GetBlobInfo(hash1)
	if err != nil {
		t.Fatalf("Blob should still exist: %v", err)
	}
	if blobInfo.RefCount != 1 {
		t.Errorf("RefCount should be 1 after delete, got %d", blobInfo.RefCount)
	}

	// file2 should still be accessible
	reader3, _, err := store.Get("bucket1", "file2.txt")
	if err != nil {
		t.Fatalf("file2 should still exist: %v", err)
	}
	_ = reader3.Close()

	// Test 6: Delete last reference - blob should be deleted
	if err := store.Delete("bucket1", "file2.txt"); err != nil {
		t.Fatalf("Failed to delete file2: %v", err)
	}

	// Blob should no longer exist
	_, err = store.GetBlobInfo(hash1)
	if err == nil {
		t.Error("Blob should be deleted when refcount reaches 0")
	}

	// Test 7: Check stats
	stats, err := store.GetStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}
	if stats.TotalBlobs != 0 {
		t.Errorf("Should have 0 blobs, got %d", stats.TotalBlobs)
	}
	if stats.TotalReferences != 0 {
		t.Errorf("Should have 0 references, got %d", stats.TotalReferences)
	}
}

func TestDedupStats(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dedup-stats-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(Options{
		BlobPath:   tmpDir + "/blobs",
		DBPath:     tmpDir + "/db",
		SyncWrites: false,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Store same content 5 times
	content := []byte("Deduplicated content!")
	for i := 0; i < 5; i++ {
		_, _, err := store.Store("bucket", "file"+string(rune('a'+i))+".txt", bytes.NewReader(content))
		if err != nil {
			t.Fatalf("Failed to store: %v", err)
		}
	}

	stats, err := store.GetStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	// Should have 1 blob, 5 references
	if stats.TotalBlobs != 1 {
		t.Errorf("Expected 1 blob, got %d", stats.TotalBlobs)
	}
	if stats.TotalReferences != 5 {
		t.Errorf("Expected 5 references, got %d", stats.TotalReferences)
	}

	// Check deduplication savings
	contentSize := int64(len(content))
	if stats.TotalBytes != contentSize {
		t.Errorf("TotalBytes should be %d, got %d", contentSize, stats.TotalBytes)
	}
	if stats.LogicalBytes != contentSize*5 {
		t.Errorf("LogicalBytes should be %d, got %d", contentSize*5, stats.LogicalBytes)
	}
	if stats.BytesSaved != contentSize*4 {
		t.Errorf("BytesSaved should be %d, got %d", contentSize*4, stats.BytesSaved)
	}

	// 80% deduplication (4 of 5 copies saved)
	expectedPct := 80.0
	if stats.DeduplicationPct != expectedPct {
		t.Errorf("DeduplicationPct should be %.1f, got %.1f", expectedPct, stats.DeduplicationPct)
	}

	t.Logf("Dedup stats: %+v", stats)
}
