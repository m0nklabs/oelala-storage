// Package webhook provides async webhook notification delivery for storage events.
package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// EventType represents the type of storage event.
type EventType string

const (
	EventFileUploaded  EventType = "file.uploaded"
	EventFileDeleted   EventType = "file.deleted"
	EventQuotaWarning  EventType = "quota.warning"
	EventQuotaExceeded EventType = "quota.exceeded"
	EventFileExpiring  EventType = "file.expiring"
	EventGCCompleted   EventType = "gc.completed"
)

// Event represents a webhook event payload.
type Event struct {
	Event     EventType   `json:"event"`
	Timestamp time.Time   `json:"timestamp"`
	NodeID    string      `json:"node_id,omitempty"`
	Data      interface{} `json:"data"`
}

// FileEventData contains data for file-related events.
type FileEventData struct {
	Bucket      string `json:"bucket"`
	Key         string `json:"key"`
	Size        int64  `json:"size,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Hash        string `json:"hash,omitempty"`
	UserID      string `json:"user_id,omitempty"`
}

// QuotaEventData contains data for quota events.
type QuotaEventData struct {
	UserID    string  `json:"user_id"`
	UsedBytes int64   `json:"used_bytes"`
	MaxBytes  int64   `json:"max_bytes"`
	Percent   float64 `json:"percent"`
}

// GCEventData contains data for garbage collection events.
type GCEventData struct {
	FilesRemoved   int    `json:"files_removed"`
	BytesRecovered int64  `json:"bytes_recovered"`
	Duration       string `json:"duration"`
}

// Config holds webhook configuration.
type Config struct {
	Enabled bool           `mapstructure:"enabled"`
	Targets []TargetConfig `mapstructure:"targets"`
	Retry   RetryConfig    `mapstructure:"retry"`
}

// TargetConfig holds a single webhook target URL and its filters.
type TargetConfig struct {
	URL    string      `mapstructure:"url"`
	Secret string      `mapstructure:"secret"` // HMAC signing secret
	Events []EventType `mapstructure:"events"` // Empty = all events
}

// RetryConfig holds retry settings.
type RetryConfig struct {
	MaxAttempts int `mapstructure:"max_attempts"` // Default: 3
	BackoffSecs int `mapstructure:"backoff_secs"` // Default: 5
}

// Dispatcher manages async webhook delivery.
type Dispatcher struct {
	config Config
	nodeID string
	queue  chan deliveryJob
	client *http.Client
	wg     sync.WaitGroup
	stopCh chan struct{}
}

type deliveryJob struct {
	target  TargetConfig
	payload []byte
	attempt int
}

// NewDispatcher creates a webhook dispatcher.
func NewDispatcher(config Config, nodeID string) *Dispatcher {
	if config.Retry.MaxAttempts == 0 {
		config.Retry.MaxAttempts = 3
	}
	if config.Retry.BackoffSecs == 0 {
		config.Retry.BackoffSecs = 5
	}

	d := &Dispatcher{
		config: config,
		nodeID: nodeID,
		queue:  make(chan deliveryJob, 256),
		client: &http.Client{Timeout: 10 * time.Second},
		stopCh: make(chan struct{}),
	}

	// Start worker goroutines
	d.wg.Add(2)
	go d.worker()
	go d.worker()

	return d
}

// Emit sends an event to all matching webhook targets.
func (d *Dispatcher) Emit(eventType EventType, data interface{}) {
	if !d.config.Enabled || len(d.config.Targets) == 0 {
		return
	}

	event := Event{
		Event:     eventType,
		Timestamp: time.Now().UTC(),
		NodeID:    d.nodeID,
		Data:      data,
	}

	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("[webhook] failed to marshal event: %v", err)
		return
	}

	for _, target := range d.config.Targets {
		if !matchesFilter(target.Events, eventType) {
			continue
		}
		select {
		case d.queue <- deliveryJob{target: target, payload: payload, attempt: 1}:
		default:
			log.Printf("[webhook] queue full, dropping event %s for %s", eventType, target.URL)
		}
	}
}

// Stop gracefully shuts down the dispatcher.
func (d *Dispatcher) Stop() {
	close(d.stopCh)
	d.wg.Wait()
}

func (d *Dispatcher) worker() {
	defer d.wg.Done()
	for {
		select {
		case <-d.stopCh:
			return
		case job := <-d.queue:
			d.deliver(job)
		}
	}
}

func (d *Dispatcher) deliver(job deliveryJob) {
	req, err := http.NewRequest(http.MethodPost, job.target.URL, bytes.NewReader(job.payload))
	if err != nil {
		log.Printf("[webhook] invalid URL %s: %v", job.target.URL, err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "oelala-storage/1.0")

	// HMAC signature
	if job.target.Secret != "" {
		sig := sign(job.payload, job.target.Secret)
		req.Header.Set("X-Webhook-Signature", "sha256="+sig)
	}

	resp, err := d.client.Do(req)
	if err != nil || (resp != nil && resp.StatusCode >= 400) {
		status := 0
		if resp != nil {
			status = resp.StatusCode
			resp.Body.Close()
		}
		if job.attempt < d.config.Retry.MaxAttempts {
			backoff := time.Duration(job.attempt*d.config.Retry.BackoffSecs) * time.Second
			log.Printf("[webhook] delivery failed (attempt %d/%d, status %d) to %s, retrying in %s",
				job.attempt, d.config.Retry.MaxAttempts, status, job.target.URL, backoff)
			time.Sleep(backoff)
			job.attempt++
			select {
			case d.queue <- job:
			default:
				log.Printf("[webhook] retry queue full, giving up on %s", job.target.URL)
			}
		} else {
			log.Printf("[webhook] delivery failed after %d attempts to %s", d.config.Retry.MaxAttempts, job.target.URL)
		}
		return
	}
	if resp != nil {
		resp.Body.Close()
	}
}

func sign(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func matchesFilter(events []EventType, target EventType) bool {
	if len(events) == 0 {
		return true // No filter = all events
	}
	for _, e := range events {
		if e == target {
			return true
		}
	}
	return false
}

// Verify checks if a webhook signature is valid. Useful for consumers.
func Verify(payload []byte, signatureHeader string, secret string) bool {
	if len(signatureHeader) < 8 || signatureHeader[:7] != "sha256=" {
		return false
	}
	expected := sign(payload, secret)
	return hmac.Equal([]byte(signatureHeader[7:]), []byte(expected))
}

// String returns a human-readable description.
func (e EventType) String() string {
	return string(e)
}

// FormatSize formats bytes as a human-readable string.
func FormatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
