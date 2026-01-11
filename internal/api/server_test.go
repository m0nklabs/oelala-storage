package api

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/m0nklabs/oelala-storage/internal/auth"
	"github.com/m0nklabs/oelala-storage/internal/storage"
)

func setupTestServer(t *testing.T) *Server {
	tmpDir := t.TempDir()
	store, err := storage.NewStore(tmpDir, 100)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	return NewServer(store, 0)
}

func TestHealthCheck(t *testing.T) {
	server := setupTestServer(t)

	req := httptest.NewRequest("GET", "/health", nil)
	resp, err := server.app.Test(req)
	if err != nil {
		t.Fatalf("Test request failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Status = %d, want 200", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body["status"] != "healthy" {
		t.Errorf("status = %v, want 'healthy'", body["status"])
	}
	if body["service"] != "oelala-storage" {
		t.Errorf("service = %v, want 'oelala-storage'", body["service"])
	}
}

func TestStatus(t *testing.T) {
	server := setupTestServer(t)

	req := httptest.NewRequest("GET", "/status", nil)
	resp, err := server.app.Test(req)
	if err != nil {
		t.Fatalf("Test request failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Status = %d, want 200", resp.StatusCode)
	}

	var body map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&body)

	if body["status"] != "running" {
		t.Errorf("status = %v, want 'running'", body["status"])
	}
}

func TestPutObject(t *testing.T) {
	server := setupTestServer(t)

	t.Run("uploads object successfully", func(t *testing.T) {
		content := "Hello, Storage!"
		req := httptest.NewRequest("PUT", "/test-bucket/hello.txt", strings.NewReader(content))
		req.Header.Set("Content-Type", "text/plain")

		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("Test request failed: %v", err)
		}

		if resp.StatusCode != 201 {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("Status = %d, want 201. Body: %s", resp.StatusCode, body)
		}
	})

	t.Run("rejects empty body", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/test-bucket/empty.txt", strings.NewReader(""))

		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("Test request failed: %v", err)
		}

		if resp.StatusCode != 400 {
			t.Errorf("Status = %d, want 400 for empty body", resp.StatusCode)
		}
	})
}

func TestGetObject(t *testing.T) {
	server := setupTestServer(t)

	content := "Content to retrieve"
	putReq := httptest.NewRequest("PUT", "/get-bucket/retrieve.txt", strings.NewReader(content))
	_, _ = server.app.Test(putReq)

	t.Run("retrieves uploaded object", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/get-bucket/retrieve.txt", nil)
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("Test request failed: %v", err)
		}

		if resp.StatusCode != 200 {
			t.Errorf("Status = %d, want 200", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if string(body) != content {
			t.Errorf("Body = %q, want %q", string(body), content)
		}
	})

	t.Run("returns 404 for non-existent object", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/get-bucket/nonexistent.txt", nil)
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("Test request failed: %v", err)
		}

		if resp.StatusCode != 404 {
			t.Errorf("Status = %d, want 404", resp.StatusCode)
		}
	})
}

func TestDeleteObject(t *testing.T) {
	server := setupTestServer(t)

	putReq := httptest.NewRequest("PUT", "/del-bucket/todelete.txt", strings.NewReader("Delete me"))
	_, _ = server.app.Test(putReq)

	t.Run("deletes existing object", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/del-bucket/todelete.txt", nil)
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("Test request failed: %v", err)
		}

		if resp.StatusCode != 204 {
			t.Errorf("Status = %d, want 204", resp.StatusCode)
		}

		getReq := httptest.NewRequest("GET", "/del-bucket/todelete.txt", nil)
		getResp, _ := server.app.Test(getReq)
		if getResp.StatusCode != 404 {
			t.Error("Object should be deleted")
		}
	})

	t.Run("returns 404 for non-existent object", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/del-bucket/nonexistent.txt", nil)
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("Test request failed: %v", err)
		}

		if resp.StatusCode != 404 {
			t.Errorf("Status = %d, want 404", resp.StatusCode)
		}
	})
}

func TestHeadObject(t *testing.T) {
	server := setupTestServer(t)

	content := "Head test content"
	putReq := httptest.NewRequest("PUT", "/head-bucket/headtest.txt", strings.NewReader(content))
	_, _ = server.app.Test(putReq)

	t.Run("returns metadata for existing object", func(t *testing.T) {
		req := httptest.NewRequest("HEAD", "/head-bucket/headtest.txt", nil)
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("Test request failed: %v", err)
		}

		if resp.StatusCode != 200 {
			t.Errorf("Status = %d, want 200", resp.StatusCode)
		}

		contentLength := resp.Header.Get("Content-Length")
		if contentLength == "" {
			t.Error("Content-Length header should be set")
		}
	})

	t.Run("returns 404 for non-existent object", func(t *testing.T) {
		req := httptest.NewRequest("HEAD", "/head-bucket/nonexistent.txt", nil)
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("Test request failed: %v", err)
		}

		if resp.StatusCode != 404 {
			t.Errorf("Status = %d, want 404", resp.StatusCode)
		}
	})
}

func TestListObjects(t *testing.T) {
	server := setupTestServer(t)

	// Use simple keys without nested paths for now (routing limitation)
	files := []struct {
		key     string
		content string
	}{
		{"file1.txt", "File 1"},
		{"file2.txt", "File 2"},
		{"file3.txt", "File 3"},
		{"imgfile.png", "Image 1"},
	}

	for _, f := range files {
		req := httptest.NewRequest("PUT", "/list-bucket/"+f.key, strings.NewReader(f.content))
		resp, _ := server.app.Test(req)
		if resp.StatusCode != 201 {
			t.Logf("Warning: PUT %s returned %d", f.key, resp.StatusCode)
		}
	}

	t.Run("lists all objects in bucket", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/list-bucket", nil)
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("Test request failed: %v", err)
		}

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("Status = %d, want 200. Body: %s", resp.StatusCode, body)
			return
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if body["bucket"] != "list-bucket" {
			t.Errorf("bucket = %v, want 'list-bucket'", body["bucket"])
		}

		if count, ok := body["count"].(float64); ok {
			if int(count) != 4 {
				t.Errorf("count = %v, want 4", count)
			}
		} else {
			t.Errorf("count field missing or wrong type")
		}
	})

	t.Run("filters by prefix", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/list-bucket?prefix=file", nil)
		resp, err := server.app.Test(req)
		if err != nil {
			t.Fatalf("Test request failed: %v", err)
		}

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("Status = %d, want 200. Body: %s", resp.StatusCode, body)
			return
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		if count, ok := body["count"].(float64); ok {
			if int(count) != 3 {
				t.Errorf("count = %v, want 3 for prefix 'file'", count)
			}
		} else {
			t.Errorf("count field missing or wrong type")
		}
	})
}

func TestPeerManagement(t *testing.T) {
	server := setupTestServer(t)

	t.Run("list peers returns empty", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/peers", nil)
		resp, _ := server.app.Test(req)

		if resp.StatusCode != 200 {
			t.Errorf("Status = %d, want 200", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		peers := body["peers"].([]interface{})
		if len(peers) != 0 {
			t.Errorf("Expected 0 peers, got %d", len(peers))
		}
	})

	t.Run("add peer returns not implemented", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/peers", strings.NewReader(`{"url":"http://peer.local"}`))
		resp, _ := server.app.Test(req)

		if resp.StatusCode != 501 {
			t.Errorf("Status = %d, want 501 (Not Implemented)", resp.StatusCode)
		}
	})

	t.Run("remove peer returns not implemented", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/peers/some-id", nil)
		resp, _ := server.app.Test(req)

		if resp.StatusCode != 501 {
			t.Errorf("Status = %d, want 501 (Not Implemented)", resp.StatusCode)
		}
	})
}

func BenchmarkHealthCheck(b *testing.B) {
	tmpDir := b.TempDir()
	store, _ := storage.NewStore(tmpDir, 100)
	server := NewServer(store, 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/health", nil)
		_, _ = server.app.Test(req)
	}
}

func TestAuthenticatedServer(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.NewStore(tmpDir, 100)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	authConfig := auth.Config{
		APIKeys: map[string]*auth.UserContext{
			"test-api-key": {
				UserID: "user123",
				TierID: "pro",
				Roles:  []string{"write"},
			},
		},
		SkipPaths: []string{"/health", "/status"},
	}

	server := NewServer(store, 0, WithAuth(authConfig))

	t.Run("health is accessible without auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		resp, _ := server.app.Test(req)

		if resp.StatusCode != 200 {
			t.Errorf("Status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("storage requires auth", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/bucket/test.txt", strings.NewReader("content"))
		resp, _ := server.app.Test(req)

		if resp.StatusCode != 401 {
			t.Errorf("Status = %d, want 401 (Unauthorized)", resp.StatusCode)
		}
	})

	t.Run("valid API key allows access", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/bucket/test.txt", strings.NewReader("content"))
		req.Header.Set("X-API-Key", "test-api-key")
		resp, _ := server.app.Test(req)

		if resp.StatusCode != 201 {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("Status = %d, want 201. Body: %s", resp.StatusCode, body)
		}
	})

	t.Run("invalid API key returns 401", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/bucket/test.txt", strings.NewReader("content"))
		req.Header.Set("X-API-Key", "wrong-key")
		resp, _ := server.app.Test(req)

		if resp.StatusCode != 401 {
			t.Errorf("Status = %d, want 401 (Unauthorized)", resp.StatusCode)
		}
	})

	t.Run("Bearer token auth works", func(t *testing.T) {
		authConfigWithToken := auth.Config{
			APIKeys: make(map[string]*auth.UserContext),
			TokenValidator: func(token string) *auth.UserContext {
				if token == "valid-jwt-token" {
					return &auth.UserContext{
						UserID: "jwt-user",
						TierID: "creator",
					}
				}
				return nil
			},
			SkipPaths: []string{"/health", "/status"},
		}

		serverWithJWT := NewServer(store, 0, WithAuth(authConfigWithToken))

		req := httptest.NewRequest("PUT", "/bucket/jwt-test.txt", strings.NewReader("jwt content"))
		req.Header.Set("Authorization", "Bearer valid-jwt-token")
		resp, _ := serverWithJWT.app.Test(req)

		if resp.StatusCode != 201 {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("Status = %d, want 201. Body: %s", resp.StatusCode, body)
		}
	})
}
