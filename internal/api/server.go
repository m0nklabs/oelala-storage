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
	"github.com/m0nklabs/oelala-storage/internal/metrics"
	"github.com/m0nklabs/oelala-storage/internal/storage"
)

// Server handles HTTP API requests
type Server struct {
	app           *fiber.App
	store         *storage.Store
	port          int
	authConfig    *auth.Config
	tlsConfig     *tls.Config
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
	bucket := c.Params("bucket")
	key := c.Params("key")

	body := c.Body()
	if len(body) == 0 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": "empty body",
		})
	}

	// Use bytes.Reader for in-memory body (works in tests and small uploads)
	// For large uploads, streaming would be handled differently
	reader := bytes.NewReader(body)

	obj, err := s.store.Put(bucket, key, reader)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
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
	defer reader.Close()

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
	bucket := c.Params("bucket")
	key := c.Params("key")

	if err := s.store.Delete(bucket, key); err != nil {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
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
	reader.Close()

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
