// Package api provides HTTP API server implementation for S3-compatible object storage.
//
//nolint:revive // api is a meaningful package name for the HTTP API server
package api

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
	"github.com/m0nklabs/oelala-storage/internal/webhook"
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
	webhooks       *webhook.Dispatcher
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

// WithWebhooks sets the webhook dispatcher
func WithWebhooks(d *webhook.Dispatcher) ServerOption {
	return func(s *Server) {
		s.webhooks = d
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
	app.Use(cors.New(cors.Config{
		AllowOrigins:     "https://oelala.xyz,http://oelala.xyz,http://localhost:5174,http://localhost:5173,http://localhost:3000,http://192.168.1.2:5174,http://192.168.1.35:5174",
		AllowMethods:     "GET,HEAD,OPTIONS",
		AllowHeaders:     "Origin,Content-Type,Accept",
		AllowCredentials: false,
		MaxAge:           86400,
	}))

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
	// PUT/DELETE require "writer" role; GET/HEAD/LIST require "reader" role
	s.app.Put("/:bucket/*", requirePermission("writer"), s.putObject)
	s.app.Post("/:bucket/*", requirePermission("writer"), s.postObject)
	s.app.Get("/:bucket/*", requirePermission("reader"), s.getObject)
	s.app.Delete("/:bucket/*", requirePermission("writer"), s.deleteObject)
	s.app.Head("/:bucket/*", requirePermission("reader"), s.headObject)
	s.app.Get("/:bucket", requirePermission("reader"), s.listObjects)
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
	response := fiber.Map{
		"status": "running",
	}

	// Add dedup stats if available
	if s.dedupStore != nil {
		if stats, err := s.dedupStore.GetStats(); err == nil {
			response["dedup"] = stats
		}
	}

	// Add GC stats if available
	if s.gcGetStats != nil {
		response["gc"] = s.gcGetStats()
	}

	return c.JSON(response)
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
		// Detect content type from file extension and body
		contentType, _, _ = storage.DetectFromReader(bytes.NewReader(body), key)
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

	// Record upload metrics
	metrics.RecordUpload(bucketName, contentType, size)

	// Emit webhook event
	if s.webhooks != nil {
		s.webhooks.Emit(webhook.EventFileUploaded, webhook.FileEventData{
			Bucket:      bucketName,
			Key:         key,
			Size:        size,
			ContentType: contentType,
			Hash:        hash,
			UserID:      userID,
		})
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

// POST /:bucket/* - Handle actions like 'move'
func (s *Server) postObject(c *fiber.Ctx) error {
	action := c.Query("action")
	if action == "move" {
		return s.moveObject(c)
	}
	return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "unsupported action"})
}

func (s *Server) moveObject(c *fiber.Ctx) error {
	srcBucket := c.Params("bucket")
	srcKey := c.Params("*")

	var req struct {
		DestBucket string `json:"dest_bucket"`
		DestKey    string `json:"dest_key"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid json payload"})
	}

	if req.DestBucket == "" {
		req.DestBucket = srcBucket // Default to same bucket
	}
	if req.DestKey == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "dest_key is required"})
	}

	useDedup := srcBucket == "dedup" || (len(srcBucket) > 6 && srcBucket[:6] == "dedup/")

	if useDedup && s.dedupStore != nil {
		err := s.dedupStore.Move(srcBucket, srcKey, req.DestBucket, req.DestKey)
		if err != nil {
			if err.Error() == "source not found" {
				return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "source object not found"})
			}
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("move failed: %v", err)})
		}
	} else {
		// Use standard storage move
		if !s.store.Exists(srcBucket, srcKey) {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "source object not found"})
		}
		err := s.store.Move(srcBucket, srcKey, req.DestBucket, req.DestKey)
		if err != nil {
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("move failed: %v", err)})
		}
	}

	return c.JSON(fiber.Map{
		"message": "moved successfully",
		"source":  srcBucket + "/" + srcKey,
		"dest":    req.DestBucket + "/" + req.DestKey,
	})
}

// GET /:bucket/* - Download object with Range support, ETag, Cache-Control
func (s *Server) getObject(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	key := c.Params("*")

	// If no key specified, list objects instead
	if key == "" {
		return s.listObjects(c)
	}

	// Check if this was stored with deduplication
	useDedup := bucketName == "dedup" || (len(bucketName) > 6 && bucketName[:6] == "dedup/")

	if useDedup && s.dedupStore != nil {
		return s.getObjectDedup(c, bucketName, key)
	}

	// Regular storage: use serveFile for Range support
	return s.serveFile(c, bucketName, key)
}

// serveFile serves a file with Range requests, ETag, Cache-Control, and Content-Disposition support.
func (s *Server) serveFile(c *fiber.Ctx, bucketName, key string) error {
	filePath := s.store.FilePath(bucketName, key)

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "object not found"})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	totalSize := stat.Size()
	modTime := stat.ModTime()

	// Determine content type
	contentType := ""
	if s.metadataStore != nil {
		if meta, mErr := s.metadataStore.Get(bucketName, key); mErr == nil && meta.ContentType != "" {
			contentType = meta.ContentType
		}
	}
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(key))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// ETag based on size + modification time (fast, no full-file hash)
	etag := fmt.Sprintf(`"%x-%x"`, modTime.UnixNano(), totalSize)

	// Check If-None-Match
	if match := c.Get("If-None-Match"); match != "" && match == etag {
		return c.SendStatus(http.StatusNotModified)
	}

	// Set standard headers
	c.Set("ETag", etag)
	c.Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
	c.Set("Accept-Ranges", "bytes")
	c.Set("Cache-Control", "public, max-age=86400") // 24h
	c.Set("Content-Type", contentType)
	setContentDisposition(c, key, contentType)

	// Parse Range header
	rangeHeader := c.Get("Range")
	if rangeHeader == "" {
		// Full response
		c.Set("Content-Length", strconv.FormatInt(totalSize, 10))
		c.Status(http.StatusOK)
		_, err = io.Copy(c.Response().BodyWriter(), file)
		if err == nil {
			metrics.RecordDownload(bucketName, totalSize)
		}
		return err
	}

	// Parse "bytes=start-end" range
	start, end, ok := parseRange(rangeHeader, totalSize)
	if !ok {
		c.Set("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
		return c.SendStatus(http.StatusRequestedRangeNotSatisfiable)
	}

	partSize := end - start + 1
	c.Set("Content-Length", strconv.FormatInt(partSize, 10))
	c.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, totalSize))
	c.Status(http.StatusPartialContent)

	if _, err = file.Seek(start, io.SeekStart); err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_, err = io.CopyN(c.Response().BodyWriter(), file, partSize)
	if err == nil {
		metrics.RecordDownload(bucketName, partSize)
	}
	return err
}

// getObjectDedup serves a deduplicated object (no Range support yet for dedup blobs).
func (s *Server) getObjectDedup(c *fiber.Ctx, bucketName, key string) error {
	reader, hash, err := s.dedupStore.Get(bucketName, key)
	if err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	}
	defer func() { _ = reader.Close() }()

	blobInfo, _ := s.dedupStore.GetBlobInfo(hash)
	var downloadSize int64
	if blobInfo != nil {
		downloadSize = blobInfo.Size
		c.Set("Content-Length", strconv.FormatInt(blobInfo.Size, 10))
	}
	c.Set("X-Blob-Hash", hash)
	c.Set("ETag", `"`+hash+`"`)
	c.Set("Cache-Control", "public, max-age=86400")

	// Content-type from metadata
	if s.metadataStore != nil {
		if meta, mErr := s.metadataStore.Get(bucketName, key); mErr == nil && meta.ContentType != "" {
			c.Set("Content-Type", meta.ContentType)
			setContentDisposition(c, key, meta.ContentType)
		}
	}

	_, err = io.Copy(c.Response().BodyWriter(), reader)
	if err == nil && downloadSize > 0 {
		metrics.RecordDownload(bucketName, downloadSize)
	}
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

	// Emit webhook event
	if s.webhooks != nil {
		s.webhooks.Emit(webhook.EventFileDeleted, webhook.FileEventData{
			Bucket: bucketName,
			Key:    key,
			Size:   fileSize,
			UserID: userID,
		})
	}

	return c.SendStatus(http.StatusNoContent)
}

// HEAD /:bucket/* - Object metadata with Content-Type, ETag, Accept-Ranges
func (s *Server) headObject(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	key := c.Params("*")

	if !s.store.Exists(bucketName, key) {
		return c.SendStatus(http.StatusNotFound)
	}

	reader, obj, err := s.store.Get(bucketName, key)
	if err != nil {
		return c.SendStatus(http.StatusNotFound)
	}
	_ = reader.Close()

	c.Set("Content-Length", strconv.FormatInt(obj.Size, 10))
	c.Set("Accept-Ranges", "bytes")

	// Content-type from metadata or mime
	contentType := ""
	if s.metadataStore != nil {
		if meta, mErr := s.metadataStore.Get(bucketName, key); mErr == nil && meta.ContentType != "" {
			contentType = meta.ContentType
		}
	}
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(key))
	}
	if contentType != "" {
		c.Set("Content-Type", contentType)
	}

	// ETag
	etag := fmt.Sprintf(`"%x-%x"`, obj.ModifiedAt.UnixNano(), obj.Size)
	c.Set("ETag", etag)
	c.Set("Last-Modified", obj.ModifiedAt.UTC().Format(http.TimeFormat))

	return c.SendStatus(http.StatusOK)
}

// GET /:bucket - List objects with pagination (max_keys, marker, delimiter)
func (s *Server) listObjects(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	prefix := c.Query("prefix", "")
	delimiter := c.Query("delimiter", "")
	marker := c.Query("marker", "")
	maxKeys, _ := strconv.Atoi(c.Query("max_keys", "1000"))
	if maxKeys <= 0 {
		maxKeys = 1000
	}
	if maxKeys > 10000 {
		maxKeys = 10000
	}

	objects, err := s.store.List(bucketName, prefix)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// Sort by key for consistent marker-based pagination
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })

	// Apply marker (skip everything <= marker)
	startIdx := 0
	if marker != "" {
		for i, obj := range objects {
			if obj.Key > marker {
				startIdx = i
				break
			}
			if i == len(objects)-1 {
				startIdx = len(objects) // marker is beyond all keys
			}
		}
	}

	// Handle delimiter (virtual directories)
	var commonPrefixes []string
	var filteredObjects []*storage.Object

	if delimiter != "" {
		seen := make(map[string]bool)
		for idx := startIdx; idx < len(objects); idx++ {
			obj := objects[idx]
			rel := obj.Key
			if prefix != "" {
				rel = strings.TrimPrefix(obj.Key, prefix)
			}
			if delimIdx := strings.Index(rel, delimiter); delimIdx >= 0 {
				cp := prefix + rel[:delimIdx+len(delimiter)]
				if !seen[cp] {
					seen[cp] = true
					commonPrefixes = append(commonPrefixes, cp)
				}
			} else {
				filteredObjects = append(filteredObjects, obj)
			}
		}
	} else {
		if startIdx < len(objects) {
			filteredObjects = objects[startIdx:]
		}
	}

	// Apply max_keys
	isTruncated := false
	nextMarker := ""
	if len(filteredObjects) > maxKeys {
		isTruncated = true
		nextMarker = filteredObjects[maxKeys-1].Key
		filteredObjects = filteredObjects[:maxKeys]
	}

	response := fiber.Map{
		"bucket":       bucketName,
		"prefix":       prefix,
		"objects":      filteredObjects,
		"count":        len(filteredObjects),
		"max_keys":     maxKeys,
		"is_truncated": isTruncated,
	}
	if nextMarker != "" {
		response["next_marker"] = nextMarker
	}
	if marker != "" {
		response["marker"] = marker
	}
	if len(commonPrefixes) > 0 {
		response["common_prefixes"] = commonPrefixes
	}

	return c.JSON(response)
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

// =============================================================================
// Helper functions
// =============================================================================

// parseRange parses an HTTP Range header like "bytes=0-1023".
// Returns start, end (inclusive), and ok.
func parseRange(rangeHeader string, totalSize int64) (int64, int64, bool) {
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}

	var start, end int64
	var err error

	if parts[0] == "" {
		// Suffix range: bytes=-500 (last 500 bytes)
		suffix, sErr := strconv.ParseInt(parts[1], 10, 64)
		if sErr != nil || suffix <= 0 {
			return 0, 0, false
		}
		start = totalSize - suffix
		if start < 0 {
			start = 0
		}
		end = totalSize - 1
	} else {
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil || start < 0 || start >= totalSize {
			return 0, 0, false
		}
		if parts[1] == "" {
			end = totalSize - 1
		} else {
			end, err = strconv.ParseInt(parts[1], 10, 64)
			if err != nil || end < start {
				return 0, 0, false
			}
			if end >= totalSize {
				end = totalSize - 1
			}
		}
	}

	return start, end, true
}

// setContentDisposition sets Content-Disposition header based on query params and content type.
func setContentDisposition(c *fiber.Ctx, key, contentType string) {
	filename := filepath.Base(key)
	download := c.Query("download", "")

	if download == "true" || download == "1" {
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		return
	}

	// For non-browser-safe types, default to attachment
	if contentType != "" &&
		!strings.HasPrefix(contentType, "image/") &&
		!strings.HasPrefix(contentType, "video/") &&
		!strings.HasPrefix(contentType, "audio/") &&
		!strings.HasPrefix(contentType, "text/") &&
		contentType != "application/pdf" {
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		return
	}

	// Inline for browser-safe types
	c.Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filename))
}

// requirePermission returns middleware that checks user permission.
// Permissions: "reader" allows GET/HEAD/LIST; "writer" allows PUT/DELETE.
// Accepts both short ("read"/"write") and long ("reader"/"writer") forms.
// If auth is not enabled (no user context), the request passes through.
func requirePermission(permission string) fiber.Handler {
	// Build the set of role names that satisfy this permission
	var accepted []string
	switch permission {
	case "reader", "read":
		accepted = []string{"reader", "read", "admin"}
	case "writer", "write":
		accepted = []string{"writer", "write", "admin"}
	default:
		accepted = []string{permission, "admin"}
	}

	return func(c *fiber.Ctx) error {
		user := auth.GetUser(c)
		if user == nil {
			// No auth middleware configured; allow (backward-compatible)
			return c.Next()
		}

		// API keys without explicit roles default to full access (backward-compatible)
		if len(user.Roles) == 0 {
			return c.Next()
		}

		for _, role := range user.Roles {
			for _, a := range accepted {
				if role == a {
					return c.Next()
				}
			}
		}

		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"error": fmt.Sprintf("insufficient permissions: %s required", permission),
		})
	}
}

// computeFileHash computes SHA-256 hash of a file for use as ETag.
func computeFileHash(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
