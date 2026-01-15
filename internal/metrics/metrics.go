// Package metrics provides Prometheus metrics collection for oelala-storage.
// It tracks object storage statistics, HTTP requests, sync operations, and quota usage.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// ObjectsTotal tracks the total number of objects stored by bucket.
	ObjectsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "oelala_storage_objects_total",
			Help: "Total number of objects stored",
		},
		[]string{"bucket"},
	)

	// StorageBytes tracks total storage used in bytes per bucket.
	StorageBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "oelala_storage_bytes",
			Help: "Total storage used in bytes",
		},
		[]string{"bucket"},
	)

	// RequestsTotal tracks the total number of HTTP requests.
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "oelala_storage_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status"},
	)

	// RequestDuration tracks HTTP request latency in seconds.
	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "oelala_storage_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "endpoint"},
	)

	// UploadBytes tracks total bytes uploaded per bucket and content type.
	UploadBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "oelala_storage_upload_bytes_total",
			Help: "Total bytes uploaded",
		},
		[]string{"bucket", "content_type"},
	)

	// DownloadBytes tracks total bytes downloaded per bucket.
	DownloadBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "oelala_storage_download_bytes_total",
			Help: "Total bytes downloaded",
		},
		[]string{"bucket"},
	)

	// SyncObjectsTotal tracks total objects synced per peer and direction.
	SyncObjectsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "oelala_storage_sync_objects_total",
			Help: "Total objects synced",
		},
		[]string{"peer_id", "direction"},
	)

	// SyncBytes tracks total bytes synced per peer and direction.
	SyncBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "oelala_storage_sync_bytes_total",
			Help: "Total bytes synced",
		},
		[]string{"peer_id", "direction"},
	)

	// SyncErrors tracks total sync errors per peer.
	SyncErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "oelala_storage_sync_errors_total",
			Help: "Total sync errors",
		},
		[]string{"peer_id"},
	)

	// PeersConnected tracks the number of connected peers.
	PeersConnected = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "oelala_storage_peers_connected",
			Help: "Number of connected peers",
		},
	)

	// QuotaBytes tracks storage quota in bytes per user and tier.
	QuotaBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "oelala_storage_quota_bytes",
			Help: "Storage quota in bytes",
		},
		[]string{"user_id", "tier"},
	)

	// QuotaUsedBytes tracks storage quota used in bytes per user and tier.
	QuotaUsedBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "oelala_storage_quota_used_bytes",
			Help: "Storage quota used in bytes",
		},
		[]string{"user_id", "tier"},
	)

	// === Garbage Collection Metrics ===

	// GCRunsTotal tracks total number of GC runs.
	GCRunsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "oelala_storage_gc_runs_total",
			Help: "Total number of garbage collection runs",
		},
	)

	// GCFilesDeleted tracks total files deleted by GC.
	GCFilesDeleted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "oelala_storage_gc_files_deleted_total",
			Help: "Total files deleted by garbage collection",
		},
	)

	// GCBytesFreed tracks total bytes freed by GC.
	GCBytesFreed = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "oelala_storage_gc_bytes_freed_total",
			Help: "Total bytes freed by garbage collection",
		},
	)

	// GCErrors tracks total GC errors.
	GCErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "oelala_storage_gc_errors_total",
			Help: "Total garbage collection errors",
		},
	)

	// GCLastRunTimestamp tracks when GC last ran.
	GCLastRunTimestamp = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "oelala_storage_gc_last_run_timestamp",
			Help: "Unix timestamp of last GC run",
		},
	)

	// GCLastRunDuration tracks duration of last GC run.
	GCLastRunDuration = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "oelala_storage_gc_last_run_duration_seconds",
			Help: "Duration of last GC run in seconds",
		},
	)

	// === Deduplication Metrics ===

	// DedupBlobsTotal tracks total unique blobs stored.
	DedupBlobsTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "oelala_storage_dedup_blobs_total",
			Help: "Total unique blobs stored (deduplicated)",
		},
	)

	// DedupRefsTotal tracks total references to blobs.
	DedupRefsTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "oelala_storage_dedup_refs_total",
			Help: "Total references to deduplicated blobs",
		},
	)

	// DedupBytesStored tracks actual bytes stored after dedup.
	DedupBytesStored = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "oelala_storage_dedup_bytes_stored",
			Help: "Actual bytes stored after deduplication",
		},
	)

	// DedupBytesLogical tracks logical bytes (before dedup).
	DedupBytesLogical = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "oelala_storage_dedup_bytes_logical",
			Help: "Logical bytes before deduplication",
		},
	)

	// DedupSavingsRatio tracks dedup savings as ratio (0-1).
	DedupSavingsRatio = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "oelala_storage_dedup_savings_ratio",
			Help: "Deduplication savings ratio (0=no savings, 1=100% deduped)",
		},
	)

	// DedupHits tracks cache hits (existing blob reused).
	DedupHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "oelala_storage_dedup_hits_total",
			Help: "Total deduplication cache hits (existing blob reused)",
		},
	)

	// DedupMisses tracks cache misses (new blob stored).
	DedupMisses = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "oelala_storage_dedup_misses_total",
			Help: "Total deduplication cache misses (new blob stored)",
		},
	)
)

// Init registers all Prometheus metrics collectors.
// Must be called before using any metrics recording functions.
func Init() {
	prometheus.MustRegister(
		ObjectsTotal, StorageBytes, RequestsTotal, RequestDuration,
		UploadBytes, DownloadBytes, SyncObjectsTotal, SyncBytes,
		SyncErrors, PeersConnected, QuotaBytes, QuotaUsedBytes,
		// GC metrics
		GCRunsTotal, GCFilesDeleted, GCBytesFreed, GCErrors,
		GCLastRunTimestamp, GCLastRunDuration,
		// Dedup metrics
		DedupBlobsTotal, DedupRefsTotal, DedupBytesStored, DedupBytesLogical,
		DedupSavingsRatio, DedupHits, DedupMisses,
	)
}

// Handler returns the Prometheus HTTP handler for exposing metrics.
func Handler() http.Handler {
	return promhttp.Handler()
}

// RecordRequest records metrics for an HTTP request.
func RecordRequest(method, endpoint, status string, duration float64) {
	RequestsTotal.WithLabelValues(method, endpoint, status).Inc()
	RequestDuration.WithLabelValues(method, endpoint).Observe(duration)
}

// RecordUpload records metrics for an object upload operation.
func RecordUpload(bucket, contentType string, bytes int64) {
	UploadBytes.WithLabelValues(bucket, contentType).Add(float64(bytes))
}

// RecordDownload records metrics for an object download operation.
func RecordDownload(bucket string, bytes int64) {
	DownloadBytes.WithLabelValues(bucket).Add(float64(bytes))
}

// RecordSync records metrics for a sync operation with a peer.
func RecordSync(peerID, direction string, objects int64, bytes int64) {
	SyncObjectsTotal.WithLabelValues(peerID, direction).Add(float64(objects))
	SyncBytes.WithLabelValues(peerID, direction).Add(float64(bytes))
}

// RecordSyncError records a sync error with a specific peer.
func RecordSyncError(peerID string) {
	SyncErrors.WithLabelValues(peerID).Inc()
}

// UpdateStorageMetrics updates the current storage metrics for a bucket.
func UpdateStorageMetrics(bucket string, objects int64, bytes int64) {
	ObjectsTotal.WithLabelValues(bucket).Set(float64(objects))
	StorageBytes.WithLabelValues(bucket).Set(float64(bytes))
}

// UpdateQuotaMetrics updates quota metrics for a user.
func UpdateQuotaMetrics(userID, tier string, quota, used int64) {
	QuotaBytes.WithLabelValues(userID, tier).Set(float64(quota))
	QuotaUsedBytes.WithLabelValues(userID, tier).Set(float64(used))
}

// UpdatePeersConnected updates the number of currently connected peers.
func UpdatePeersConnected(count int) {
	PeersConnected.Set(float64(count))
}

// === Garbage Collection Helper Functions ===

// RecordGCRun records metrics for a completed GC run.
func RecordGCRun(filesDeleted, bytesFreed, errors int64, durationSeconds float64) {
	GCRunsTotal.Inc()
	GCFilesDeleted.Add(float64(filesDeleted))
	GCBytesFreed.Add(float64(bytesFreed))
	GCErrors.Add(float64(errors))
	GCLastRunTimestamp.SetToCurrentTime()
	GCLastRunDuration.Set(durationSeconds)
}

// === Deduplication Helper Functions ===

// UpdateDedupStats updates all deduplication metrics from stats.
func UpdateDedupStats(blobs, refs, bytesStored, bytesLogical int64, savingsRatio float64) {
	DedupBlobsTotal.Set(float64(blobs))
	DedupRefsTotal.Set(float64(refs))
	DedupBytesStored.Set(float64(bytesStored))
	DedupBytesLogical.Set(float64(bytesLogical))
	DedupSavingsRatio.Set(savingsRatio)
}

// RecordDedupHit records a deduplication cache hit.
func RecordDedupHit() {
	DedupHits.Inc()
}

// RecordDedupMiss records a deduplication cache miss.
func RecordDedupMiss() {
	DedupMisses.Inc()
}
