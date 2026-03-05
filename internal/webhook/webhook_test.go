package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestNewDispatcher(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Targets: []TargetConfig{
			{URL: "http://localhost:9999/hook", Secret: "test-secret"},
		},
	}
	d := NewDispatcher(cfg, "test-node")
	defer d.Stop()

	if d.config.Retry.MaxAttempts != 3 {
		t.Errorf("expected default MaxAttempts=3, got %d", d.config.Retry.MaxAttempts)
	}
	if d.config.Retry.BackoffSecs != 5 {
		t.Errorf("expected default BackoffSecs=5, got %d", d.config.Retry.BackoffSecs)
	}
}

func TestEmitDisabled(t *testing.T) {
	cfg := Config{Enabled: false}
	d := NewDispatcher(cfg, "test-node")
	defer d.Stop()

	// Should not panic or block
	d.Emit(EventFileUploaded, FileEventData{Bucket: "test", Key: "file.txt"})
}

func TestEmitNoTargets(t *testing.T) {
	cfg := Config{Enabled: true, Targets: []TargetConfig{}}
	d := NewDispatcher(cfg, "test-node")
	defer d.Stop()

	// Should not panic or block
	d.Emit(EventFileUploaded, FileEventData{Bucket: "test", Key: "file.txt"})
}

func TestEmitDelivery(t *testing.T) {
	var mu sync.Mutex
	var received []Event
	secret := "test-hmac-secret"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		// Verify HMAC signature
		sig := r.Header.Get("X-Webhook-Signature")
		if sig == "" {
			t.Error("missing X-Webhook-Signature header")
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if sig != expected {
			t.Errorf("signature mismatch: got %q, want %q", sig, expected)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		var event Event
		if err := json.Unmarshal(body, &event); err != nil {
			t.Errorf("failed to unmarshal event: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mu.Lock()
		received = append(received, event)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{
		Enabled: true,
		Targets: []TargetConfig{
			{URL: server.URL, Secret: secret},
		},
		Retry: RetryConfig{MaxAttempts: 1, BackoffSecs: 1},
	}
	d := NewDispatcher(cfg, "node-1")

	d.Emit(EventFileUploaded, FileEventData{
		Bucket:      "users/123",
		Key:         "photo.jpg",
		Size:        1024,
		ContentType: "image/jpeg",
		Hash:        "abc123",
		UserID:      "123",
	})

	// Wait for async delivery
	time.Sleep(500 * time.Millisecond)
	d.Stop()

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}

	ev := received[0]
	if ev.Event != EventFileUploaded {
		t.Errorf("expected event type %q, got %q", EventFileUploaded, ev.Event)
	}
	if ev.NodeID != "node-1" {
		t.Errorf("expected node_id %q, got %q", "node-1", ev.NodeID)
	}
}

func TestEmitEventFiltering(t *testing.T) {
	var mu sync.Mutex
	var count int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{
		Enabled: true,
		Targets: []TargetConfig{
			{
				URL:    server.URL,
				Secret: "s",
				Events: []EventType{EventFileDeleted},
			},
		},
		Retry: RetryConfig{MaxAttempts: 1, BackoffSecs: 1},
	}
	d := NewDispatcher(cfg, "node-1")

	// Upload event should be filtered out
	d.Emit(EventFileUploaded, FileEventData{Bucket: "b", Key: "k"})
	// Delete event should be delivered
	d.Emit(EventFileDeleted, FileEventData{Bucket: "b", Key: "k"})

	time.Sleep(500 * time.Millisecond)
	d.Stop()

	mu.Lock()
	defer mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 delivery (only delete), got %d", count)
	}
}

func TestVerify(t *testing.T) {
	secret := "verify-secret"
	body := []byte(`{"event":"file.uploaded","data":{}}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !Verify(body, sig, secret) {
		t.Error("Verify returned false for valid signature")
	}

	if Verify(body, "sha256=invalid", secret) {
		t.Error("Verify returned true for invalid signature")
	}

	if Verify(body, "invalid-format", secret) {
		t.Error("Verify returned true for malformed signature")
	}
}
