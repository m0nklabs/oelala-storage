// Package api provides HTTP API server implementation for S3-compatible object storage.
//
//nolint:revive // api is a meaningful package name for the HTTP API server
package api

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/m0nklabs/oelala-storage/internal/auth"
	"github.com/m0nklabs/oelala-storage/internal/bucket"
	"github.com/m0nklabs/oelala-storage/internal/dedup"
	"github.com/m0nklabs/oelala-storage/internal/metadata"
	"github.com/m0nklabs/oelala-storage/internal/metrics"
	"github.com/m0nklabs/oelala-storage/internal/signedurl"
	"github.com/m0nklabs/oelala-storage/internal/storage"
)

// Server handles HTTP API requests
type Server struct {
	app            *fiber.App
	store          *storage.Store
	metadataStore  *metadata.Store
	dedupStore     *dedup.Store
	bucketStore    *bucket.Store
	port           int
	authConfig     *auth.Config
	tlsConfig      *tls.Config
	metricsEnabled bool
	adminServer    *AdminServer
	signer         *signedurl.Signer
	baseURL        string
	gcRunOnce      func() interface{} // For triggering GC via API
	gcGetStats     func() interface{} // For getting GC stats
}

// ServerOption configures the server
type ServerOption func(*Server)

// WithAuth enables authentication
func WithAuth(config auth.Config) ServerOption {
	return func(s *Server) {
		s.authConfig = &config
	}
}

// WithTLS enables TLS
func WithTLS(config *tls.Config) ServerOption {
	return func(s *Server) {
		s.tlsConfig = config
	}
}

// WithMetrics enables metrics middleware
func WithMetrics() ServerOption {
	return func(s *Server) {
		s.metricsEnabled = true
	}
}

// WithBucketStore sets the bucket store
func WithBucketStore(bs *bucket.Store) ServerOption {
	return func(s *Server) {
		s.bucketStore = bs
	}
}

// WithAdminServer sets the admin server for API key management
func WithAdminServer(admin *AdminServer) ServerOption {
	return func(s *Server) {
		s.adminServer = admin
	}
}

// WithSigningSecret enables signed URL generation
func WithSigningSecret(secret string, baseURL string) ServerOption {
	return func(s *Server) {
		if secret != "" {
			s.signer = signedurl.NewSigner(secret)
			s.baseURL = baseURL
		}
	}
}

// WithMetadataStore sets the metadata store for expiration tracking
func WithMetadataStore(ms *metadata.Store) ServerOption {
	return func(s *Server) {
		s.metadataStore = ms
	}
}

// WithDedupStore sets the deduplication store
func WithDedupStore(ds *dedup.Store) ServerOption {
	return func(s *Server) {
		s.dedupStore = ds
	}
}

// NewServer creates a new API server
func NewServer(store *storage.Store, port int, opts ...ServerOption) *Server {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		BodyLimit:             100 * 1024 * 1024 * 1024, // 100GB max upload
	})

	// Middleware
	app.Use(recover.New())
	app.Use(logger.New())
	app.Use(cors.New())

	s := &Server{
		app:   app,
		store: store,
		port:  port,
	}

	// Apply options
	for _, opt := range opts {
		opt(s)
	}

	// Setup admin routes first (before auth middleware)
	if s.adminServer != nil {
		s.adminServer.SetupRoutes(app)
	}

	// Apply auth middleware if configured
	if s.authConfig != nil {
		app.Use(auth.New(*s.authConfig))
	}

	// Apply metrics middleware if enabled
	if s.metricsEnabled {
		app.Use(metricsMiddleware())
	}

	s.setupRoutes()
	return s
}

// metricsMiddleware records request metrics
func metricsMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		duration := time.Since(start).Seconds()

		status := strconv.Itoa(c.Response().StatusCode())
		metrics.RecordRequest(c.Method(), c.Path(), status, duration)
		return err
	}
}

func (s *Server) setupRoutes() {
	// Health check (most specific first)
	s.app.Get("/health", s.healthCheck)
	s.app.Get("/status", s.status)

	// GC management routes (requires auth)
	s.app.Get("/gc/stats", s.gcStats)
	s.app.Post("/gc/run", s.gcRun)

	// Deduplication stats route
	s.app.Get("/dedup/stats", s.dedupStats)

	// Bucket management routes
	s.app.Post("/buckets", s.createBucket)
	s.app.Get("/buckets", s.listBuckets)
	s.app.Get("/buckets/:user_id", s.getBucket)
	s.app.Patch("/buckets/:user_id", s.updateBucket)
	s.app.Delete("/buckets/:user_id", s.deleteBucket)

	// Management routes (before bucket wildcards)
	s.app.Get("/peers", s.listPeers)
	s.app.Post("/peers", s.addPeer)
	s.app.Delete("/peers/:id", s.removePeer)

	// Signed URL routes (before bucket wildcards, but require signer)
	if s.signer != nil {
		s.app.Post("/signed-url", s.createSignedURL)
		s.app.Get("/s/:bucket/*", s.getSignedObject) // Public: no auth required
	}

	// S3-compatible routes (with wildcard for nested keys)
	// Fiber uses :key* for wildcard params that catch everything including slashes
	s.app.Put("/:bucket/*", s.putObject)
	s.app.Get("/:bucket/*", s.getObject)
	s.app.Delete("/:bucket/*", s.deleteObject)
	s.app.Head("/:bucket/*", s.headObject)
	s.app.Get("/:bucket", s.listObjects)
}

// gcStats returns garbage collection statistics
func (s *Server) gcStats(c *fiber.Ctx) error {
	if s.gcGetStats == nil {
		return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "garbage collector not configured",
		})
	}
	return c.JSON(s.gcGetStats())
}

// gcRun triggers a garbage collection run
func (s *Server) gcRun(c *fiber.Ctx) error {
	if s.gcRunOnce == nil {
		return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "garbage collector not configured",
		})
	}
	stats := s.gcRunOnce()
	return c.JSON(stats)
}

// dedupStats returns deduplication statistics
func (s *Server) dedupStats(c *fiber.Ctx) error {
	if s.dedupStore == nil {
		return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "deduplication not configured",
		})
	}
	stats, err := s.dedupStore.GetStats()
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	return c.JSON(stats)
}

// Start begins listening for requests
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.port)
	if s.tlsConfig != nil {
		fmt.Printf("📦 HTTPS API listening on %s (TLS enabled)\n", addr)
		ln, err := tls.Listen("tcp", addr, s.tlsConfig)
		if err != nil {
			return err
		}
		return s.app.Listener(ln)
	}
	fmt.Printf("📦 HTTP API listening on %s\n", addr)
	return s.app.Listen(addr)
}

// Stop gracefully shuts down the server
func (s *Server) Stop() error {
	return s.app.Shutdown()
}

// SetGCFunctions sets the garbage collector functions for API access
func (s *Server) SetGCFunctions(runOnce func() interface{}, getStats func() interface{}) {
	s.gcRunOnce = runOnce
	s.gcGetStats = getStats
}

// Health check endpoint
func (s *Server) healthCheck(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status":  "healthy",
		"service": "oelala-storage",
	})
}

// Status endpoint
func (s *Server) status(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status": "running",
		// TODO: Add disk usage, peer count, etc.
	})
}

// PUT /:bucket/* - Upload object
func (s *Server) putObject(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	key := c.Params("*")

	body := c.Body()
	if len(body) == 0 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "empty body",
		})
	}

	// Parse X-Expires-At header (ISO 8601 format)
	// This is set by the backend to determine when files should be garbage collected
	var expiresAt *time.Time
	if expiresAtStr := c.Get("X-Expires-At"); expiresAtStr != "" {
		parsed, err := time.Parse(time.RFC3339, expiresAtStr)
		if err == nil {
			expiresAt = &parsed
		}
	}

	// Get user ID from header or extract from bucket path
	userID := c.Get("X-User-ID")
	if userID == "" && len(bucketName) > 6 && bucketName[:6] == "users/" {
		// Extract user ID from path (format: users/{user_id}/...)
		parts := bytes.Split([]byte(bucketName), []byte("/"))
		if len(parts) >= 2 {
			userID = string(parts[1])
		}
	}

	// Check quota before upload
	if s.bucketStore != nil && userID != "" {
		hasQuota, info, _ := s.bucketStore.CheckQuota(userID, int64(len(body)))
		if !hasQuota {
			return c.Status(http.StatusPaymentRequired).JSON(fiber.Map{
				"error":       "storage_quota_exceeded",
				"used":        info.UsedBytes,
				"limit":       info.QuotaBytes,
				"upgrade_url": "https://oelala.ai/upgrade",
			})
		}
	}

	// Check if client requests deduplication (or if bucket starts with "dedup/")
	useDedup := c.Get("X-Dedup") == "true" || bucketName == "dedup" || (len(bucketName) > 6 && bucketName[:6] == "dedup/")

	var hash string
	var size int64
	var contentType string
	var createdAt time.Time

	if useDedup && s.dedupStore != nil {
		// Use content-addressed storage with deduplication
		reader := bytes.NewReader(body)
		var err error
		hash, size, err = s.dedupStore.Store(bucketName, key, reader)
		if err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}
		contentType = "application/octet-stream" // TODO: detect content type
		createdAt = time.Now()
	} else {
		// Use regular file storage (backward compatible)
		reader := bytes.NewReader(body)
		obj, err := s.store.Put(bucketName, key, reader)
		if err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}
		hash = obj.Hash
		size = obj.Size
		contentType = obj.ContentType
		createdAt = obj.CreatedAt
	}

	// Store metadata if metadata store is available
	if s.metadataStore != nil {
		meta := &metadata.ObjectMeta{
			Key:         key,
			Bucket:      bucketName,
			Size:        size,
			ContentType: contentType,
			Hash:        hash,
			UserID:      userID,
			CreatedAt:   createdAt,
			ModifiedAt:  createdAt,
			ExpiresAt:   expiresAt,
		}
		if err := s.metadataStore.Put(meta); err != nil {
			// Log but don't fail - file is already stored
			// In production, we might want to be stricter here
		}
	}

	// Update usage after successful upload
	if s.bucketStore != nil && userID != "" {
		_ = s.bucketStore.AddUsage(userID, size, 1)
	}

	// Include expiration in response if set
	response := fiber.Map{
		"key":          key,
		"bucket":       bucketName,
		"size":         size,
		"content_type": contentType,
		"hash":         hash,
		"created_at":   createdAt,
	}
	if expiresAt != nil {
		response["expires_at"] = expiresAt
	}
	if useDedup {
		response["deduplicated"] = true
	}

	return c.Status(http.StatusCreated).JSON(response)
}

// GET /:bucket/* - Download object
func (s *Server) getObject(c *fiber.Ctx) error {
	bucket := c.Params("bucket")
	key := c.Params("*")

	// If no key specified, list objects instead
	if key == "" {
		return s.listObjects(c)
	}

	// Check if this was stored with deduplication
	useDedup := bucket == "dedup" || (len(bucket) > 6 && bucket[:6] == "dedup/")

	if useDedup && s.dedupStore != nil {
		reader, hash, err := s.dedupStore.Get(bucket, key)
		if err != nil {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": err.Error(),
			})
		}
		defer func() { _ = reader.Close() }()

		// Get size from blob info
		blobInfo, _ := s.dedupStore.GetBlobInfo(hash)
		if blobInfo != nil {
			c.Set("Content-Length", fmt.Sprintf("%d", blobInfo.Size))
		}
		c.Set("X-Blob-Hash", hash)

		// Stream the file
		_, err = io.Copy(c.Response().BodyWriter(), reader)
		return err
	}

	// Regular storage
	reader, obj, err := s.store.Get(bucket, key)
	if err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	defer func() { _ = reader.Close() }()

	c.Set("Content-Length", fmt.Sprintf("%d", obj.Size))
	if obj.ContentType != "" {
		c.Set("Content-Type", obj.ContentType)
	}

	// Stream the file
	_, err = io.Copy(c.Response().BodyWriter(), reader)
	return err
}

// DELETE /:bucket/* - Delete object
func (s *Server) deleteObject(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	key := c.Params("*")

	// Get file size before deleting for quota update
	var fileSize int64
	var userID string

	// Extract user ID from bucket name or header
	userID = c.Get("X-User-ID")
	if userID == "" && len(bucketName) > 6 && bucketName[:6] == "users/" {
		parts := bytes.Split([]byte(bucketName), []byte("/"))
		if len(parts) >= 2 {
			userID = string(parts[1])
		}
	}

	// Get size before delete (from metadata store if available, otherwise file)
	if s.metadataStore != nil {
		if meta, err := s.metadataStore.Get(bucketName, key); err == nil {
			fileSize = meta.Size
		}
	}

	// Check if this was stored with deduplication
	useDedup := bucketName == "dedup" || (len(bucketName) > 6 && bucketName[:6] == "dedup/")

	if useDedup && s.dedupStore != nil {
		// Delete from dedup store (handles ref counting)
		if err := s.dedupStore.Delete(bucketName, key); err != nil {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": err.Error(),
			})
		}
	} else {
		// Get size from file if not in metadata
		if fileSize == 0 && s.bucketStore != nil && userID != "" {
			reader, obj, err := s.store.Get(bucketName, key)
			if err == nil {
				fileSize = obj.Size
				_ = reader.Close()
			}
		}

		// Delete from file storage
		if err := s.store.Delete(bucketName, key); err != nil {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{
				"error": err.Error(),
			})
		}
	}

	// Delete from metadata store
	if s.metadataStore != nil {
		_ = s.metadataStore.Delete(bucketName, key)
	}

	// Update usage after successful delete
	if s.bucketStore != nil && userID != "" && fileSize > 0 {
		_ = s.bucketStore.AddUsage(userID, -fileSize, -1)
	}

	return c.SendStatus(http.StatusNoContent)
}

// HEAD /:bucket/* - Object metadata
func (s *Server) headObject(c *fiber.Ctx) error {
	bucket := c.Params("bucket")
	key := c.Params("*")

	if !s.store.Exists(bucket, key) {
		return c.SendStatus(http.StatusNotFound)
	}

	reader, obj, err := s.store.Get(bucket, key)
	if err != nil {
		return c.SendStatus(http.StatusNotFound)
	}
	_ = reader.Close()

	c.Set("Content-Length", fmt.Sprintf("%d", obj.Size))
	return c.SendStatus(http.StatusOK)
}

// GET /:bucket - List objects
func (s *Server) listObjects(c *fiber.Ctx) error {
	bucket := c.Params("bucket")
	prefix := c.Query("prefix", "")

	objects, err := s.store.List(bucket, prefix)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"bucket":  bucket,
		"prefix":  prefix,
		"objects": objects,
		"count":   len(objects),
	})
}

// Peer management (stubs for now)
func (s *Server) listPeers(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"peers": []interface{}{},
	})
}

func (s *Server) addPeer(c *fiber.Ctx) error {
	return c.Status(http.StatusNotImplemented).JSON(fiber.Map{
		"error": "not implemented",
	})
}

func (s *Server) removePeer(c *fiber.Ctx) error {
	return c.Status(http.StatusNotImplemented).JSON(fiber.Map{
		"error": "not implemented",
	})
}

// Bucket management handlers

// POST /buckets - Create user bucket
func (s *Server) createBucket(c *fiber.Ctx) error {
	if s.bucketStore == nil {
		return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "bucket management not enabled",
		})
	}

	var req bucket.CreateRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}

	if req.UserID == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "user_id is required",
		})
	}

	info, err := s.bucketStore.Create(&req)
	if err != nil {
		return c.Status(http.StatusConflict).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(http.StatusCreated).JSON(info)
}

// GET /buckets - List all buckets (admin)
func (s *Server) listBuckets(c *fiber.Ctx) error {
	if s.bucketStore == nil {
		return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "bucket management not enabled",
		})
	}

	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))

	buckets, err := s.bucketStore.List(limit, offset)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"buckets": buckets,
		"count":   len(buckets),
		"limit":   limit,
		"offset":  offset,
	})
}

// GET /buckets/:user_id - Get bucket info
func (s *Server) getBucket(c *fiber.Ctx) error {
	if s.bucketStore == nil {
		return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "bucket management not enabled",
		})
	}

	userID := c.Params("user_id")
	info, err := s.bucketStore.Get(userID)
	if err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Add quota headers
	c.Set("X-Quota-Used", strconv.FormatInt(info.UsedBytes, 10))
	c.Set("X-Quota-Limit", strconv.FormatInt(info.QuotaBytes, 10))

	return c.JSON(info)
}

// PATCH /buckets/:user_id - Update bucket tier/quota
func (s *Server) updateBucket(c *fiber.Ctx) error {
	if s.bucketStore == nil {
		return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "bucket management not enabled",
		})
	}

	userID := c.Params("user_id")

	var req bucket.UpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}

	info, err := s.bucketStore.Update(userID, &req)
	if err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(info)
}

// DELETE /buckets/:user_id - Delete bucket (admin)
func (s *Server) deleteBucket(c *fiber.Ctx) error {
	if s.bucketStore == nil {
		return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "bucket management not enabled",
		})
	}

	userID := c.Params("user_id")

	if err := s.bucketStore.Delete(userID); err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.SendStatus(http.StatusNoContent)
}

// =============================================================================
// Signed URL Endpoints
// =============================================================================

// SignedURLRequest is the request body for creating a signed URL
type SignedURLRequest struct {
	Bucket       string `json:"bucket"`
	Key          string `json:"key"`
	ExpiresIn    int    `json:"expires_in"`    // seconds (default: 3600)
	MaxDownloads int    `json:"max_downloads"` // optional, 0 = unlimited
}

// POST /signed-url - Create a signed URL for accessing an object
func (s *Server) createSignedURL(c *fiber.Ctx) error {
	if s.signer == nil {
		return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "signed URLs not configured",
		})
	}

	var req SignedURLRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}

	if req.Bucket == "" || req.Key == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "bucket and key are required",
		})
	}

	// Default expiration: 1 hour
	expiresIn := time.Duration(req.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = time.Hour
	}
	// Max: 7 days
	if expiresIn > 7*24*time.Hour {
		expiresIn = 7 * 24 * time.Hour
	}

	// Verify the object exists
	reader, obj, err := s.store.Get(req.Bucket, req.Key)
	if err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": "object not found",
		})
	}
	_ = reader.Close() // Just checking existence
	_ = obj

	params := signedurl.SignedURLParams{
		Bucket:       req.Bucket,
		Key:          req.Key,
		ExpiresIn:    expiresIn,
		MaxDownloads: req.MaxDownloads,
	}

	baseURL := s.baseURL
	if baseURL == "" {
		// Fallback: use request host
		scheme := "http"
		if c.Protocol() == "https" {
			scheme = "https"
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, c.Hostname())
	}

	result, err := s.signer.Generate(baseURL, params)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"url":        result.URL,
		"expires_at": result.ExpiresAt.Format(time.RFC3339),
		"bucket":     req.Bucket,
		"key":        req.Key,
	})
}

// GET /s/:bucket/* - Access object via signed URL (public, no auth)
func (s *Server) getSignedObject(c *fiber.Ctx) error {
	if s.signer == nil {
		return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "signed URLs not configured",
		})
	}

	bucket := c.Params("bucket")
	key := c.Params("*")
	sig := c.Query("sig")
	exp := c.Query("exp")
	maxStr := c.Query("max")

	// Verify signature
	params, err := s.signer.Verify(bucket, key, sig, exp, maxStr)
	if err != nil {
		status := http.StatusForbidden
		if err == signedurl.ErrExpired {
			status = http.StatusGone // 410 Gone for expired URLs
		}
		return c.Status(status).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Get the object
	reader, obj, err := s.store.Get(params.Bucket, params.Key)
	if err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": "object not found",
		})
	}
	defer func() { _ = reader.Close() }()

	// Set headers
	c.Set("Content-Length", strconv.FormatInt(obj.Size, 10))
	if obj.ContentType != "" {
		c.Set("Content-Type", obj.ContentType)
	}
	c.Set("Cache-Control", "public, max-age=86400") // Cache for 24h

	// Stream the file
	_, err = io.Copy(c.Response().BodyWriter(), reader)
	return err
}
