// Package gc provides garbage collection for expired objects in oelala-storage.
//
// The garbage collector runs periodically and deletes objects that have passed
// their expiration time (set via X-Expires-At header on upload).
//
// Architecture note: The backend (oelala) determines retention policies based on
// user tier. Storage just executes what the backend tells it. Storage is "dumb".
package gc

import (
	"context"
	"sync"
	"time"

	"github.com/m0nklabs/oelala-storage/internal/logging"
	"github.com/m0nklabs/oelala-storage/internal/metadata"
	"github.com/m0nklabs/oelala-storage/internal/metrics"
	"github.com/m0nklabs/oelala-storage/internal/storage"
	"go.uber.org/zap"
)

// Stats contains garbage collection statistics
type Stats struct {
	LastRun       time.Time `json:"last_run"`
	FilesDeleted  int64     `json:"files_deleted"`
	BytesFreed    int64     `json:"bytes_freed"`
	Errors        int64     `json:"errors"`
	TotalRuns     int64     `json:"total_runs"`
	RunDurationMs int64     `json:"run_duration_ms"`
}

// Collector handles garbage collection of expired objects
type Collector struct {
	store         *storage.Store
	metadataStore *metadata.Store
	interval      time.Duration
	dryRun        bool

	mu    sync.RWMutex
	stats Stats

	stopCh chan struct{}
	doneCh chan struct{}
}

// Config for the garbage collector
type Config struct {
	Interval time.Duration
	DryRun   bool
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		Interval: 1 * time.Hour,
		DryRun:   false,
	}
}

// NewCollector creates a new garbage collector
func NewCollector(store *storage.Store, metadataStore *metadata.Store, cfg Config) *Collector {
	return &Collector{
		store:         store,
		metadataStore: metadataStore,
		interval:      cfg.Interval,
		dryRun:        cfg.DryRun,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}
}

// Start begins the periodic garbage collection
func (c *Collector) Start(ctx context.Context) {
	logging.Info("🗑️ Garbage collector started",
		zap.Duration("interval", c.interval),
		zap.Bool("dry_run", c.dryRun),
	)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Run once immediately
	c.RunOnce()

	for {
		select {
		case <-ctx.Done():
			close(c.doneCh)
			return
		case <-c.stopCh:
			close(c.doneCh)
			return
		case <-ticker.C:
			c.RunOnce()
		}
	}
}

// Stop stops the garbage collector
func (c *Collector) Stop() {
	close(c.stopCh)
	<-c.doneCh
}

// RunOnce performs a single garbage collection pass
func (c *Collector) RunOnce() Stats {
	start := time.Now()

	// Get all expired objects
	expired, err := c.metadataStore.ListExpired()
	if err != nil {
		logging.Error("Failed to list expired objects", zap.Error(err))
		c.mu.Lock()
		c.stats.Errors++
		c.mu.Unlock()
		return c.GetStats()
	}

	if len(expired) == 0 {
		logging.Debug("No expired objects to collect")
		c.mu.Lock()
		c.stats.LastRun = time.Now()
		c.stats.TotalRuns++
		c.mu.Unlock()
		return c.GetStats()
	}

	logging.Info("🗑️ Found expired objects",
		zap.Int("count", len(expired)),
		zap.Bool("dry_run", c.dryRun),
	)

	var filesDeleted, bytesFreed int64
	var errors int64

	for _, meta := range expired {
		if c.dryRun {
			logging.Info("Would delete (dry-run)",
				zap.String("bucket", meta.Bucket),
				zap.String("key", meta.Key),
				zap.Int64("size", meta.Size),
				zap.Time("expires_at", *meta.ExpiresAt),
			)
			filesDeleted++
			bytesFreed += meta.Size
			continue
		}

		// Delete from file storage
		if err := c.store.Delete(meta.Bucket, meta.Key); err != nil {
			logging.Warn("Failed to delete file",
				zap.String("bucket", meta.Bucket),
				zap.String("key", meta.Key),
				zap.Error(err),
			)
			errors++
			continue
		}

		// Delete from metadata store
		if err := c.metadataStore.Delete(meta.Bucket, meta.Key); err != nil {
			logging.Warn("Failed to delete metadata",
				zap.String("bucket", meta.Bucket),
				zap.String("key", meta.Key),
				zap.Error(err),
			)
			// Continue anyway - file is already deleted
		}

		filesDeleted++
		bytesFreed += meta.Size

		logging.Debug("Deleted expired object",
			zap.String("bucket", meta.Bucket),
			zap.String("key", meta.Key),
			zap.Int64("size", meta.Size),
		)
	}

	duration := time.Since(start)

	c.mu.Lock()
	c.stats.LastRun = time.Now()
	c.stats.FilesDeleted += filesDeleted
	c.stats.BytesFreed += bytesFreed
	c.stats.Errors += errors
	c.stats.TotalRuns++
	c.stats.RunDurationMs = duration.Milliseconds()
	c.mu.Unlock()

	// Record Prometheus metrics
	metrics.RecordGCRun(filesDeleted, bytesFreed, errors, duration.Seconds())

	logging.Info("🗑️ Garbage collection complete",
		zap.Int64("files_deleted", filesDeleted),
		zap.Int64("bytes_freed", bytesFreed),
		zap.Int64("errors", errors),
		zap.Duration("duration", duration),
	)

	return c.GetStats()
}

// GetStats returns current GC statistics
func (c *Collector) GetStats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

// SetDryRun enables/disables dry run mode
func (c *Collector) SetDryRun(dryRun bool) {
	c.dryRun = dryRun
}
