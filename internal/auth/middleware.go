// Package auth provides authentication and authorization middleware for the HTTP API.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// UserContext contains authenticated user information
type UserContext struct {
	UserID    string    `json:"user_id"`
	TierID    string    `json:"tier_id"`
	Roles     []string  `json:"roles"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// Config holds auth middleware configuration
type Config struct {
	APIKeys        map[string]*UserContext
	TokenValidator func(token string) *UserContext
	SkipPaths      []string
	ErrorHandler   fiber.ErrorHandler
	SigningSecret  string // For signed URL verification (HMAC-SHA256)
}

// DefaultConfig returns default auth configuration
func DefaultConfig() Config {
	return Config{
		APIKeys:   make(map[string]*UserContext),
		SkipPaths: []string{"/health", "/status"},
		ErrorHandler: func(c *fiber.Ctx, _ error) error {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthorized",
			})
		},
	}
}

const contextKey = "user"

// New creates a new auth middleware
func New(config ...Config) fiber.Handler {
	cfg := DefaultConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	if cfg.ErrorHandler == nil {
		cfg.ErrorHandler = DefaultConfig().ErrorHandler
	}

	return func(c *fiber.Ctx) error {
		path := c.Path()
		for _, skip := range cfg.SkipPaths {
			if path == skip || strings.HasPrefix(path, skip) {
				return c.Next()
			}
		}

		// Check for signed URL authentication (public access via signature)
		expires := c.Query("expires")
		sig := c.Query("sig")
		if expires != "" && sig != "" && cfg.SigningSecret != "" {
			if user := validateSignedURL(path, expires, sig, cfg.SigningSecret); user != nil {
				c.Locals(contextKey, user)
				return c.Next()
			}
			// Invalid signature - return 401
			return cfg.ErrorHandler(c, fiber.ErrUnauthorized)
		}

		auth := c.Get("Authorization")
		if auth == "" {
			auth = c.Get("X-API-Key")
			if auth != "" {
				auth = "ApiKey " + auth
			}
		}

		if auth == "" {
			return cfg.ErrorHandler(c, fiber.ErrUnauthorized)
		}

		var user *UserContext

		if strings.HasPrefix(auth, "Bearer ") {
			token := strings.TrimPrefix(auth, "Bearer ")
			// Try static keys first (Bearer can also be API key)
			user = validateAPIKey(cfg.APIKeys, token)
			// Fall back to dynamic validator
			if user == nil && cfg.TokenValidator != nil {
				user = cfg.TokenValidator(token)
			}
		} else if strings.HasPrefix(auth, "ApiKey ") {
			apiKey := strings.TrimPrefix(auth, "ApiKey ")
			// Try static keys first
			user = validateAPIKey(cfg.APIKeys, apiKey)
			// Fall back to dynamic validator (e.g., apikeys store)
			if user == nil && cfg.TokenValidator != nil {
				user = cfg.TokenValidator(apiKey)
			}
		}

		if user == nil {
			return cfg.ErrorHandler(c, fiber.ErrUnauthorized)
		}

		if !user.ExpiresAt.IsZero() && time.Now().After(user.ExpiresAt) {
			return cfg.ErrorHandler(c, fiber.ErrUnauthorized)
		}

		c.Locals(contextKey, user)
		return c.Next()
	}
}

func validateAPIKey(keys map[string]*UserContext, key string) *UserContext {
	for k, user := range keys {
		if subtle.ConstantTimeCompare([]byte(k), []byte(key)) == 1 {
			return user
		}
	}
	return nil
}

// validateSignedURL verifies a signed URL and returns a temporary user context if valid.
// The signature is HMAC-SHA256(path:expires, secret) encoded as hex.
// Returns nil if signature is invalid or URL has expired.
func validateSignedURL(path, expiresStr, sig, secret string) *UserContext {
	// Parse expiration timestamp
	expiresUnix, err := strconv.ParseInt(expiresStr, 10, 64)
	if err != nil {
		return nil
	}

	// Check if URL has expired
	expiresTime := time.Unix(expiresUnix, 0)
	if time.Now().After(expiresTime) {
		return nil
	}

	// Compute expected signature: HMAC-SHA256(path:expires, secret)
	message := path + ":" + expiresStr
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	// Constant-time comparison to prevent timing attacks
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expectedSig)) != 1 {
		return nil
	}

	// Valid signature - return a temporary user context for public/anonymous access
	return &UserContext{
		UserID:    "anonymous",
		TierID:    "public",
		Roles:     []string{"reader"}, // Read-only access
		ExpiresAt: expiresTime,
	}
}

// GetUser retrieves the authenticated user from the request context
func GetUser(c *fiber.Ctx) *UserContext {
	user, ok := c.Locals(contextKey).(*UserContext)
	if !ok {
		return nil
	}
	return user
}

// GetUserID is a convenience function to get just the user ID
func GetUserID(c *fiber.Ctx) string {
	user := GetUser(c)
	if user == nil {
		return ""
	}
	return user.UserID
}

// RequireRole creates middleware that checks for a specific role
func RequireRole(role string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user := GetUser(c)
		if user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthorized",
			})
		}

		for _, r := range user.Roles {
			if r == role {
				return c.Next()
			}
		}

		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "forbidden: requires role " + role,
		})
	}
}

// RequireTier creates middleware that checks for minimum tier
func RequireTier(minTier string) fiber.Handler {
	tierOrder := map[string]int{
		"free":    0,
		"creator": 1,
		"pro":     2,
		"studio":  3,
	}

	return func(c *fiber.Ctx) error {
		user := GetUser(c)
		if user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthorized",
			})
		}

		userLevel := tierOrder[user.TierID]
		requiredLevel := tierOrder[minTier]

		if userLevel < requiredLevel {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":         "forbidden: requires tier " + minTier + " or higher",
				"current_tier":  user.TierID,
				"required_tier": minTier,
			})
		}

		return c.Next()
	}
}
