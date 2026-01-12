// Package api provides HTTP API server implementation for S3-compatible object storage.
package api

import (
	"embed"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/m0nklabs/oelala-storage/internal/apikeys"
)

//go:embed admin.html
var adminHTML embed.FS

// AdminServer handles admin API requests
type AdminServer struct {
	keyStore    *apikeys.Store
	adminSecret string // Simple admin password for initial setup
}

// NewAdminServer creates a new admin server
func NewAdminServer(keyStore *apikeys.Store, adminSecret string) *AdminServer {
	return &AdminServer{
		keyStore:    keyStore,
		adminSecret: adminSecret,
	}
}

// SetupRoutes adds admin routes to a Fiber app
func (a *AdminServer) SetupRoutes(app *fiber.App) {
	admin := app.Group("/admin")

	// Simple auth check for admin routes
	admin.Use(func(c *fiber.Ctx) error {
		// Check X-Admin-Secret header or query param
		secret := c.Get("X-Admin-Secret")
		if secret == "" {
			secret = c.Query("secret")
		}
		if secret == "" {
			secret = c.Cookies("admin_secret")
		}

		if a.adminSecret != "" && secret != a.adminSecret {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "admin authentication required",
				"hint":  "Set X-Admin-Secret header or ?secret= query param",
			})
		}
		return c.Next()
	})

	// Admin UI
	admin.Get("/", a.adminUI)

	// API Key management
	admin.Get("/keys", a.listKeys)
	admin.Post("/keys", a.createKey)
	admin.Delete("/keys/:id", a.deleteKey)
	admin.Post("/keys/:id/revoke", a.revokeKey)
}

// adminUI serves the admin HTML page
func (a *AdminServer) adminUI(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/html; charset=utf-8")
	data, err := adminHTML.ReadFile("admin.html")
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString("Admin UI not found")
	}
	return c.Send(data)
}

// listKeys returns all API keys
func (a *AdminServer) listKeys(c *fiber.Ctx) error {
	keys, err := a.keyStore.List()
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	return c.JSON(fiber.Map{
		"keys": keys,
	})
}

// createKey creates a new API key
func (a *AdminServer) createKey(c *fiber.Ctx) error {
	var req struct {
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}

	if req.Name == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "name is required",
		})
	}

	if len(req.Permissions) == 0 {
		req.Permissions = []string{"read", "write"}
	}

	plainKey, key, err := a.keyStore.Create(req.Name, req.Permissions)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(http.StatusCreated).JSON(fiber.Map{
		"key":     key,
		"api_key": plainKey, // Only shown once!
		"warning": "Save this API key now! It will not be shown again.",
	})
}

// deleteKey permanently deletes an API key
func (a *AdminServer) deleteKey(c *fiber.Ctx) error {
	keyID := c.Params("id")

	if err := a.keyStore.Delete(keyID); err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.SendStatus(http.StatusNoContent)
}

// revokeKey deactivates an API key without deleting it
func (a *AdminServer) revokeKey(c *fiber.Ctx) error {
	keyID := c.Params("id")

	if err := a.keyStore.Revoke(keyID); err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"message": "key revoked",
	})
}
