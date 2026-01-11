package metadata

import (
	"testing"
	"time"
)

func setupTestStore(t *testing.T) *Store {
	t.Helper()
	opts := Options{
		Path:     t.TempDir(),
		InMemory: true,
	}
	store, err := NewStore(opts)
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func TestNewStore(t *testing.T) {
	t.Run("creates store with valid path", func(t *testing.T) {
		store := setupTestStore(t)
		if store == nil {
			t.Fatal("NewStore() returned nil")
		}
	})

	t.Run("creates in-memory store", func(t *testing.T) {
		opts := Options{InMemory: true}
		store, err := NewStore(opts)
		if err != nil {
			t.Fatalf("NewStore() error = %v", err)
		}
		defer func() { _ = store.Close() }()
		if store == nil {
			t.Fatal("NewStore() returned nil for in-memory")
		}
	})
}

func TestPutAndGet(t *testing.T) {
	store := setupTestStore(t)

	t.Run("stores and retrieves metadata", func(t *testing.T) {
		now := time.Now()
		meta := &ObjectMeta{
			Key:         "test.txt",
			Bucket:      "my-bucket",
			Size:        1024,
			ContentType: "text/plain",
			Hash:        "abc123hash",
			UserID:      "user-1",
			CreatedAt:   now,
			ModifiedAt:  now,
			Tags:        map[string]string{"env": "test"},
		}

		err := store.Put(meta)
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		got, err := store.Get("my-bucket", "test.txt")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}

		if got.Key != meta.Key {
			t.Errorf("Key = %v, want %v", got.Key, meta.Key)
		}
		if got.Bucket != meta.Bucket {
			t.Errorf("Bucket = %v, want %v", got.Bucket, meta.Bucket)
		}
		if got.Size != meta.Size {
			t.Errorf("Size = %v, want %v", got.Size, meta.Size)
		}
		if got.Hash != meta.Hash {
			t.Errorf("Hash = %v, want %v", got.Hash, meta.Hash)
		}
		if got.UserID != meta.UserID {
			t.Errorf("UserID = %v, want %v", got.UserID, meta.UserID)
		}
	})

	t.Run("returns error for non-existent object", func(t *testing.T) {
		_, err := store.Get("no-bucket", "no-key")
		if err == nil {
			t.Error("Get() should return error for non-existent object")
		}
	})
}

func TestDelete(t *testing.T) {
	store := setupTestStore(t)

	t.Run("deletes existing metadata", func(t *testing.T) {
		meta := &ObjectMeta{
			Key:    "to-delete.txt",
			Bucket: "del-bucket",
			Size:   512,
			UserID: "user-1",
		}
		store.Put(meta)

		err := store.Delete("del-bucket", "to-delete.txt")
		if err != nil {
			t.Fatalf("Delete() error = %v", err)
		}

		if store.Exists("del-bucket", "to-delete.txt") {
			t.Error("Object should not exist after delete")
		}
	})

	t.Run("returns error for non-existent object", func(t *testing.T) {
		err := store.Delete("no-bucket", "no-key")
		if err == nil {
			t.Error("Delete() should return error for non-existent object")
		}
	})
}

func TestExists(t *testing.T) {
	store := setupTestStore(t)

	t.Run("returns true for existing object", func(t *testing.T) {
		meta := &ObjectMeta{
			Key:    "exists.txt",
			Bucket: "check-bucket",
		}
		store.Put(meta)

		if !store.Exists("check-bucket", "exists.txt") {
			t.Error("Exists() should return true for existing object")
		}
	})

	t.Run("returns false for non-existent object", func(t *testing.T) {
		if store.Exists("no-bucket", "no-key") {
			t.Error("Exists() should return false for non-existent object")
		}
	})
}

func TestListByBucket(t *testing.T) {
	store := setupTestStore(t)

	objects := []*ObjectMeta{
		{Key: "file1.txt", Bucket: "list-bucket", Size: 100},
		{Key: "file2.txt", Bucket: "list-bucket", Size: 200},
		{Key: "file3.txt", Bucket: "list-bucket", Size: 300},
		{Key: "other.txt", Bucket: "other-bucket", Size: 400},
	}
	for _, obj := range objects {
		store.Put(obj)
	}

	t.Run("lists all objects in bucket", func(t *testing.T) {
		results, err := store.ListByBucket("list-bucket")
		if err != nil {
			t.Fatalf("ListByBucket() error = %v", err)
		}

		if len(results) != 3 {
			t.Errorf("Expected 3 objects, got %d", len(results))
		}
	})

	t.Run("returns empty for non-existent bucket", func(t *testing.T) {
		results, err := store.ListByBucket("empty-bucket")
		if err != nil {
			t.Fatalf("ListByBucket() error = %v", err)
		}

		if len(results) != 0 {
			t.Errorf("Expected 0 objects, got %d", len(results))
		}
	})
}

func TestListByUser(t *testing.T) {
	store := setupTestStore(t)

	objects := []*ObjectMeta{
		{Key: "user1-file1.txt", Bucket: "b1", UserID: "user-1", Size: 100},
		{Key: "user1-file2.txt", Bucket: "b1", UserID: "user-1", Size: 200},
		{Key: "user2-file1.txt", Bucket: "b1", UserID: "user-2", Size: 300},
		{Key: "no-user.txt", Bucket: "b1", Size: 400},
	}
	for _, obj := range objects {
		store.Put(obj)
	}

	t.Run("lists objects for specific user", func(t *testing.T) {
		results, err := store.ListByUser("user-1")
		if err != nil {
			t.Fatalf("ListByUser() error = %v", err)
		}

		if len(results) != 2 {
			t.Errorf("Expected 2 objects for user-1, got %d", len(results))
		}
	})

	t.Run("returns empty for user with no objects", func(t *testing.T) {
		results, err := store.ListByUser("user-999")
		if err != nil {
			t.Fatalf("ListByUser() error = %v", err)
		}

		if len(results) != 0 {
			t.Errorf("Expected 0 objects, got %d", len(results))
		}
	})
}

func TestGetUserStorageUsed(t *testing.T) {
	store := setupTestStore(t)

	objects := []*ObjectMeta{
		{Key: "f1.txt", Bucket: "b1", UserID: "quota-user", Size: 1000},
		{Key: "f2.txt", Bucket: "b1", UserID: "quota-user", Size: 2000},
		{Key: "f3.txt", Bucket: "b2", UserID: "quota-user", Size: 3000},
		{Key: "other.txt", Bucket: "b1", UserID: "other-user", Size: 9999},
	}
	for _, obj := range objects {
		store.Put(obj)
	}

	t.Run("calculates total storage for user", func(t *testing.T) {
		used, err := store.GetUserStorageUsed("quota-user")
		if err != nil {
			t.Fatalf("GetUserStorageUsed() error = %v", err)
		}

		expected := int64(6000)
		if used != expected {
			t.Errorf("Used = %d, want %d", used, expected)
		}
	})

	t.Run("returns 0 for user with no objects", func(t *testing.T) {
		used, err := store.GetUserStorageUsed("empty-user")
		if err != nil {
			t.Fatalf("GetUserStorageUsed() error = %v", err)
		}

		if used != 0 {
			t.Errorf("Used = %d, want 0", used)
		}
	})
}

func TestListExpired(t *testing.T) {
	store := setupTestStore(t)

	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(1 * time.Hour)

	objects := []*ObjectMeta{
		{Key: "expired1.txt", Bucket: "b1", ExpiresAt: &past},
		{Key: "expired2.txt", Bucket: "b1", ExpiresAt: &past},
		{Key: "not-expired.txt", Bucket: "b1", ExpiresAt: &future},
		{Key: "no-expiry.txt", Bucket: "b1"},
	}
	for _, obj := range objects {
		store.Put(obj)
	}

	t.Run("returns only expired objects", func(t *testing.T) {
		expired, err := store.ListExpired()
		if err != nil {
			t.Fatalf("ListExpired() error = %v", err)
		}

		if len(expired) != 2 {
			t.Errorf("Expected 2 expired objects, got %d", len(expired))
		}

		for _, obj := range expired {
			if obj.ExpiresAt == nil || obj.ExpiresAt.After(time.Now()) {
				t.Errorf("Object %s should be expired", obj.Key)
			}
		}
	})
}

func BenchmarkPut(b *testing.B) {
	opts := Options{InMemory: true}
	store, _ := NewStore(opts)
	defer func() { _ = store.Close() }()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		meta := &ObjectMeta{
			Key:    "bench.txt",
			Bucket: "bench",
			Size:   1024,
		}
		store.Put(meta)
	}
}

func BenchmarkGet(b *testing.B) {
	opts := Options{InMemory: true}
	store, _ := NewStore(opts)
	defer func() { _ = store.Close() }()

	meta := &ObjectMeta{
		Key:    "bench.txt",
		Bucket: "bench",
		Size:   1024,
	}
	store.Put(meta)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Get("bench", "bench.txt")
	}
}
