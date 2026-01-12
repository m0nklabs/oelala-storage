package bucket

import (
	"os"
	"testing"

	"github.com/dgraph-io/badger/v4"
)

func setupTestStore(t *testing.T) (*Store, func()) {
	t.Helper()

	// Create temp directory
	dir, err := os.MkdirTemp("", "bucket-test-*")
	if err != nil {
		t.Fatal(err)
	}

	// Open BadgerDB
	opts := badger.DefaultOptions(dir)
	opts.Logger = nil
	db, err := badger.Open(opts)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatal(err)
	}

	store := NewStore(db)

	cleanup := func() {
		db.Close()
		os.RemoveAll(dir)
	}

	return store, cleanup
}

func TestCreate(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	req := &CreateRequest{
		UserID: "test-user-123",
		Tier:   "free",
	}

	info, err := store.Create(req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if info.UserID != req.UserID {
		t.Errorf("UserID = %s, want %s", info.UserID, req.UserID)
	}
	if info.Tier != "free" {
		t.Errorf("Tier = %s, want free", info.Tier)
	}
	if info.QuotaBytes != 2*1024*1024*1024 {
		t.Errorf("QuotaBytes = %d, want 2GB", info.QuotaBytes)
	}
	if info.UsedBytes != 0 {
		t.Errorf("UsedBytes = %d, want 0", info.UsedBytes)
	}
}

func TestCreateDuplicate(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	req := &CreateRequest{
		UserID: "test-user-123",
		Tier:   "free",
	}

	_, err := store.Create(req)
	if err != nil {
		t.Fatalf("First create failed: %v", err)
	}

	_, err = store.Create(req)
	if err == nil {
		t.Error("Expected error for duplicate bucket, got nil")
	}
}

func TestGet(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create bucket first
	req := &CreateRequest{
		UserID: "test-user-123",
		Tier:   "pro",
	}
	_, err := store.Create(req)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Get it back
	info, err := store.Get("test-user-123")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if info.Tier != "pro" {
		t.Errorf("Tier = %s, want pro", info.Tier)
	}
}

func TestGetNotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_, err := store.Get("nonexistent-user")
	if err == nil {
		t.Error("Expected error for nonexistent bucket, got nil")
	}
}

func TestUpdate(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create bucket first
	_, err := store.Create(&CreateRequest{
		UserID: "test-user-123",
		Tier:   "free",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Upgrade to pro
	info, err := store.Update("test-user-123", &UpdateRequest{
		Tier: "pro",
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if info.Tier != "pro" {
		t.Errorf("Tier = %s, want pro", info.Tier)
	}
	if info.QuotaBytes != 200*1024*1024*1024 {
		t.Errorf("QuotaBytes = %d, want 200GB", info.QuotaBytes)
	}
}

func TestAddUsage(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create bucket first
	_, err := store.Create(&CreateRequest{
		UserID: "test-user-123",
		Tier:   "free",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Add usage
	err = store.AddUsage("test-user-123", 1000000, 1)
	if err != nil {
		t.Fatalf("AddUsage failed: %v", err)
	}

	info, _ := store.Get("test-user-123")
	if info.UsedBytes != 1000000 {
		t.Errorf("UsedBytes = %d, want 1000000", info.UsedBytes)
	}
	if info.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1", info.FileCount)
	}
}

func TestAddUsageAutoCreate(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Add usage without creating bucket first (should auto-create)
	err := store.AddUsage("new-user", 5000, 1)
	if err != nil {
		t.Fatalf("AddUsage failed: %v", err)
	}

	info, err := store.Get("new-user")
	if err != nil {
		t.Fatalf("Get failed after auto-create: %v", err)
	}

	if info.Tier != "free" {
		t.Errorf("Tier = %s, want free", info.Tier)
	}
	if info.UsedBytes != 5000 {
		t.Errorf("UsedBytes = %d, want 5000", info.UsedBytes)
	}
}

func TestCheckQuota(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create bucket with custom quota
	_, err := store.Create(&CreateRequest{
		UserID:  "test-user-123",
		Tier:    "free",
		QuotaGB: 1, // 1GB
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Should have quota for 500MB
	hasQuota, _, err := store.CheckQuota("test-user-123", 500*1024*1024)
	if err != nil {
		t.Fatalf("CheckQuota failed: %v", err)
	}
	if !hasQuota {
		t.Error("Expected to have quota for 500MB")
	}

	// Add 900MB usage
	_ = store.AddUsage("test-user-123", 900*1024*1024, 1)

	// Should NOT have quota for 200MB more (would exceed 1GB)
	hasQuota, info, _ := store.CheckQuota("test-user-123", 200*1024*1024)
	if hasQuota {
		t.Errorf("Expected quota exceeded (used=%d, limit=%d)", info.UsedBytes, info.QuotaBytes)
	}
}

func TestList(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create multiple buckets
	for i := 0; i < 5; i++ {
		_, err := store.Create(&CreateRequest{
			UserID: "user-" + string(rune('a'+i)),
			Tier:   "free",
		})
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	// List all
	buckets, err := store.List(10, 0)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(buckets) != 5 {
		t.Errorf("len(buckets) = %d, want 5", len(buckets))
	}

	// List with limit
	buckets, err = store.List(2, 0)
	if err != nil {
		t.Fatalf("List with limit failed: %v", err)
	}
	if len(buckets) != 2 {
		t.Errorf("len(buckets) = %d, want 2", len(buckets))
	}
}

func TestDelete(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Create bucket
	_, err := store.Create(&CreateRequest{
		UserID: "test-user-123",
		Tier:   "free",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Delete it
	err = store.Delete("test-user-123")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Should be gone
	_, err = store.Get("test-user-123")
	if err == nil {
		t.Error("Expected error for deleted bucket, got nil")
	}
}
