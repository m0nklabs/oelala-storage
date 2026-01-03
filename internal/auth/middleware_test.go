package auth

import (
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func setupTestApp(cfg Config) *fiber.App {
	app := fiber.New()
	app.Use(New(cfg))
	app.Get("/protected", func(c *fiber.Ctx) error {
		user := GetUser(c)
		if user == nil {
			return c.JSON(fiber.Map{"user": nil})
		}
		return c.JSON(fiber.Map{"user_id": user.UserID, "tier": user.TierID})
	})
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	app.Get("/admin", RequireRole("admin"), func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"admin": true})
	})
	app.Get("/pro-only", RequireTier("pro"), func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"pro": true})
	})
	return app
}

func TestAuthMiddleware(t *testing.T) {
	cfg := Config{
		APIKeys: map[string]*UserContext{
			"test-key-123": {UserID: "user-1", TierID: "creator", Roles: []string{"user"}},
			"admin-key":    {UserID: "admin-1", TierID: "studio", Roles: []string{"user", "admin"}},
		},
		SkipPaths: []string{"/health"},
	}
	app := setupTestApp(cfg)

	t.Run("rejects request without auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 401 {
			t.Errorf("Status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("accepts valid API key in Authorization header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "ApiKey test-key-123")
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Errorf("Status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("accepts valid API key in X-API-Key header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("X-API-Key", "test-key-123")
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Errorf("Status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("rejects invalid API key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "ApiKey wrong-key")
		resp, _ := app.Test(req)
		if resp.StatusCode != 401 {
			t.Errorf("Status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("skips auth for configured paths", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Errorf("Status = %d, want 200", resp.StatusCode)
		}
	})
}

func TestBearerToken(t *testing.T) {
	cfg := Config{
		TokenValidator: func(token string) *UserContext {
			if token == "valid-token" {
				return &UserContext{UserID: "token-user", TierID: "pro", Roles: []string{"user"}}
			}
			return nil
		},
		SkipPaths: []string{"/health"},
	}
	app := setupTestApp(cfg)

	t.Run("accepts valid Bearer token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Errorf("Status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("rejects invalid Bearer token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		resp, _ := app.Test(req)
		if resp.StatusCode != 401 {
			t.Errorf("Status = %d, want 401", resp.StatusCode)
		}
	})
}

func TestExpiredToken(t *testing.T) {
	expired := time.Now().Add(-1 * time.Hour)
	cfg := Config{
		APIKeys:   map[string]*UserContext{"expired-key": {UserID: "user-1", TierID: "free", ExpiresAt: expired}},
		SkipPaths: []string{"/health"},
	}
	app := setupTestApp(cfg)

	t.Run("rejects expired key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "ApiKey expired-key")
		resp, _ := app.Test(req)
		if resp.StatusCode != 401 {
			t.Errorf("Status = %d, want 401", resp.StatusCode)
		}
	})
}

func TestRequireRole(t *testing.T) {
	cfg := Config{
		APIKeys: map[string]*UserContext{
			"user-key":  {UserID: "user-1", TierID: "creator", Roles: []string{"user"}},
			"admin-key": {UserID: "admin-1", TierID: "studio", Roles: []string{"user", "admin"}},
		},
		SkipPaths: []string{"/health"},
	}
	app := setupTestApp(cfg)

	t.Run("allows user with required role", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/admin", nil)
		req.Header.Set("Authorization", "ApiKey admin-key")
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Errorf("Status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("denies user without required role", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/admin", nil)
		req.Header.Set("Authorization", "ApiKey user-key")
		resp, _ := app.Test(req)
		if resp.StatusCode != 403 {
			t.Errorf("Status = %d, want 403", resp.StatusCode)
		}
	})
}

func TestRequireTier(t *testing.T) {
	cfg := Config{
		APIKeys: map[string]*UserContext{
			"free-key":    {UserID: "free-user", TierID: "free"},
			"creator-key": {UserID: "creator-user", TierID: "creator"},
			"pro-key":     {UserID: "pro-user", TierID: "pro"},
			"studio-key":  {UserID: "studio-user", TierID: "studio"},
		},
		SkipPaths: []string{"/health"},
	}
	app := setupTestApp(cfg)

	t.Run("allows user with exact tier", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/pro-only", nil)
		req.Header.Set("Authorization", "ApiKey pro-key")
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Errorf("Status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("allows user with higher tier", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/pro-only", nil)
		req.Header.Set("Authorization", "ApiKey studio-key")
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Errorf("Status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("denies user with lower tier", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/pro-only", nil)
		req.Header.Set("Authorization", "ApiKey creator-key")
		resp, _ := app.Test(req)
		if resp.StatusCode != 403 {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("Status = %d, want 403. Body: %s", resp.StatusCode, body)
		}
	})

	t.Run("denies free tier user", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/pro-only", nil)
		req.Header.Set("Authorization", "ApiKey free-key")
		resp, _ := app.Test(req)
		if resp.StatusCode != 403 {
			t.Errorf("Status = %d, want 403", resp.StatusCode)
		}
	})
}

func TestGetUserID(t *testing.T) {
	app := fiber.New()
	cfg := Config{
		APIKeys:   map[string]*UserContext{"test-key": {UserID: "user-123", TierID: "pro"}},
		SkipPaths: []string{"/unauth"},
	}
	app.Use(New(cfg))

	app.Get("/auth", func(c *fiber.Ctx) error {
		return c.SendString(GetUserID(c))
	})
	app.Get("/unauth", func(c *fiber.Ctx) error {
		return c.SendString(GetUserID(c))
	})

	t.Run("returns user ID for authenticated request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/auth", nil)
		req.Header.Set("Authorization", "ApiKey test-key")
		resp, _ := app.Test(req)
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "user-123" {
			t.Errorf("UserID = %s, want user-123", string(body))
		}
	})

	t.Run("returns empty for unauthenticated request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/unauth", nil)
		resp, _ := app.Test(req)
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "" {
			t.Errorf("UserID = %s, want empty", string(body))
		}
	})
}
