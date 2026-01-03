package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	ObjectsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "oelala_storage_objects_total",
			Help: "Total number of objects stored",
		},
		[]string{"bucket"},
	)

	StorageBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "oelala_storage_bytes",
			Help: "Total storage used in bytes",
		},
		[]string{"bucket"},
	)

	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "oelala_storage_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status"},
	)

	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "oelala_storage_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "endpoint"},
	)

	UploadBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "oelala_storage_upload_bytes_total",
			Help: "Total bytes uploaded",
		},
		[]string{"bucket", "content_type"},
	)

	DownloadBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "oelala_storage_download_bytes_total",
			Help: "Total bytes downloaded",
		},
		[]string{"bucket"},
	)

	SyncObjectsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "oelala_storage_sync_objects_total",
			Help: "Total objects synced",
		},
		[]string{"peer_id", "direction"},
	)

	SyncBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "oelala_storage_sync_bytes_total",
			Help: "Total bytes synced",
		},
		[]string{"peer_id", "direction"},
	)

	SyncErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "oelala_storage_sync_errors_total",
			Help: "Total sync errors",
		},
		[]string{"peer_id"},
	)

	PeersConnected = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "oelala_storage_peers_connected",
			Help: "Number of connected peers",
		},
	)

	QuotaBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "oelala_storage_quota_bytes",
			Help: "Storage quota in bytes",
		},
		[]string{"user_id", "tier"},
	)

	QuotaUsedBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "oelala_storage_quota_used_bytes",
			Help: "Storage quota used in bytes",
		},
		[]string{"user_id", "tier"},
	)
)

func Init() {
	prometheus.MustRegister(
		ObjectsTotal, StorageBytes, RequestsTotal, RequestDuration,
		UploadBytes, DownloadBytes, SyncObjectsTotal, SyncBytes,
		SyncErrors, PeersConnected, QuotaBytes, QuotaUsedBytes,
	)
}

func Handler() http.Handler {
	return promhttp.Handler()
}

func RecordRequest(method, endpoint, status string, duration float64) {
	RequestsTotal.WithLabelValues(method, endpoint, status).Inc()
	RequestDuration.WithLabelValues(method, endpoint).Observe(duration)
}

func RecordUpload(bucket, contentType string, bytes int64) {
	UploadBytes.WithLabelValues(bucket, contentType).Add(float64(bytes))
}

func RecordDownload(bucket string, bytes int64) {
	DownloadBytes.WithLabelValues(bucket).Add(float64(bytes))
}

func RecordSync(peerID, direction string, objects int64, bytes int64) {
	SyncObjectsTotal.WithLabelValues(peerID, direction).Add(float64(objects))
	SyncBytes.WithLabelValues(peerID, direction).Add(float64(bytes))
}

func RecordSyncError(peerID string) {
	SyncErrors.WithLabelValues(peerID).Inc()
}

func UpdateStorageMetrics(bucket string, objects int64, bytes int64) {
	ObjectsTotal.WithLabelValues(bucket).Set(float64(objects))
	StorageBytes.WithLabelValues(bucket).Set(float64(bytes))
}

func UpdateQuotaMetrics(userID, tier string, quota, used int64) {
	QuotaBytes.WithLabelValues(userID, tier).Set(float64(quota))
	QuotaUsedBytes.WithLabelValues(userID, tier).Set(float64(used))
}

func UpdatePeersConnected(count int) {
	PeersConnected.Set(float64(count))
}
