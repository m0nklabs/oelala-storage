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
	"github.com/m0nklabs/oelala-storage/internal/metrics"
	"github.com/m0nklabs/oelala-storage/internal/storage"
)

// Server handles HTTP API requests
type Server struct {
	app            *fiber.App
	store          *storage.Store
	bucketStore    *bucket.Store
	port           int
	authConfig     *auth.Config
	tlsConfig      *tls.Config
	metricsEnabled bool
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

	// S3-compatible routes (with wildcard for nested keys)
	s.app.Put("/:bucket/:key<*>", s.putObject)
	s.app.Get("/:bucket/:key<*>", s.getObject)
	s.app.Delete("/:bucket/:key<*>", s.deleteObject)
	s.app.Head("/:bucket/:key<*>", s.headObject)
	s.app.Get("/:bucket", s.listObjects)
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

// PUT /:bucket/:key - Upload object
func (s *Server) putObject(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	key := c.Params("key")

	body := c.Body()
	if len(body) == 0 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "empty body",
		})
	}

	// Check quota if bucket management is enabled
	// Extract user ID from bucket name (format: users/{user_id}/...)
	var userID string
	if len(bucketName) > 6 && bucketName[:6] == "users/" {
		// Extract user ID from path
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

	// Use bytes.Reader for in-memory body (works in tests and small uploads)
	// For large uploads, streaming would be handled differently
	reader := bytes.NewReader(body)

	obj, err := s.store.Put(bucketName, key, reader)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Update usage after successful upload
	if s.bucketStore != nil && userID != "" {
		_ = s.bucketStore.AddUsage(userID, obj.Size, 1)
	}

	return c.Status(http.StatusCreated).JSON(obj)
}

// GET /:bucket/:key - Download object
func (s *Server) getObject(c *fiber.Ctx) error {
	bucket := c.Params("bucket")
	key := c.Params("key")

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

// DELETE /:bucket/:key - Delete object
func (s *Server) deleteObject(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	key := c.Params("key")

	// Get file size before deleting for quota update
	var fileSize int64
	var userID string

	// Extract user ID from bucket name
	if len(bucketName) > 6 && bucketName[:6] == "users/" {
		parts := bytes.Split([]byte(bucketName), []byte("/"))
		if len(parts) >= 2 {
			userID = string(parts[1])
		}
	}

	// Get size before delete
	if s.bucketStore != nil && userID != "" {
		reader, obj, err := s.store.Get(bucketName, key)
		if err == nil {
			fileSize = obj.Size
			_ = reader.Close()
		}
	}

	if err := s.store.Delete(bucketName, key); err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Update usage after successful delete
	if s.bucketStore != nil && userID != "" && fileSize > 0 {
		_ = s.bucketStore.AddUsage(userID, -fileSize, -1)
	}

	return c.SendStatus(http.StatusNoContent)
}

// HEAD /:bucket/:key - Object metadata
func (s *Server) headObject(c *fiber.Ctx) error {
	bucket := c.Params("bucket")
	key := c.Params("key")

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
