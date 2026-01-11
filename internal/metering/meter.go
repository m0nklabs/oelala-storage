// Package metering provides usage event tracking and metering for billing and analytics.
package metering

import (
	"sync"
	"time"
)

// EventType represents types of metered events
type EventType string

const (
	// EventUpload represents a storage upload event.
	EventUpload EventType = "storage.upload"
	// EventDownload represents a storage download event.
	EventDownload EventType = "storage.download"
	// EventDelete represents a storage deletion event.
	EventDelete EventType = "storage.delete"
	// EventGenImage represents an image generation event.
	EventGenImage EventType = "generation.image"
	// EventGenVideo represents a video generation event.
	EventGenVideo EventType = "generation.video"
	// EventGenAudio represents an audio generation event.
	EventGenAudio EventType = "generation.audio"
)

// Event represents a metered usage event
type Event struct {
	ID        string            `json:"id"`
	UserID    string            `json:"user_id"`
	EventType EventType         `json:"event_type"`
	Bytes     int64             `json:"bytes,omitempty"`
	Credits   int               `json:"credits,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Meter tracks usage events
type Meter struct {
	events   []Event
	mu       sync.RWMutex
	handlers []func(Event)
	maxSize  int
}

// NewMeter creates a new usage meter
func NewMeter(maxEvents int) *Meter {
	if maxEvents <= 0 {
		maxEvents = 10000
	}
	return &Meter{
		events:  make([]Event, 0, maxEvents),
		maxSize: maxEvents,
	}
}

// OnEvent registers an event handler
func (m *Meter) OnEvent(handler func(Event)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers = append(m.handlers, handler)
}

// Record records a usage event
func (m *Meter) Record(event Event) {
	m.mu.Lock()

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Trim if at capacity
	if len(m.events) >= m.maxSize {
		m.events = m.events[1:]
	}
	m.events = append(m.events, event)

	handlers := m.handlers
	m.mu.Unlock()

	// Notify handlers
	for _, h := range handlers {
		h(event)
	}
}

// RecordUpload records an upload event
func (m *Meter) RecordUpload(userID, bucket, key, contentType string, bytes int64) {
	m.Record(Event{
		UserID:    userID,
		EventType: EventUpload,
		Bytes:     bytes,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"bucket":       bucket,
			"key":          key,
			"content_type": contentType,
		},
	})
}

// RecordDownload records a download event
func (m *Meter) RecordDownload(userID, bucket, key string, bytes int64) {
	m.Record(Event{
		UserID:    userID,
		EventType: EventDownload,
		Bytes:     bytes,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"bucket": bucket,
			"key":    key,
		},
	})
}

// RecordDelete records a delete event
func (m *Meter) RecordDelete(userID, bucket, key string, bytes int64) {
	m.Record(Event{
		UserID:    userID,
		EventType: EventDelete,
		Bytes:     bytes,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"bucket": bucket,
			"key":    key,
		},
	})
}

// RecordGeneration records a generation event
func (m *Meter) RecordGeneration(userID string, eventType EventType, credits int, metadata map[string]string) {
	m.Record(Event{
		UserID:    userID,
		EventType: eventType,
		Credits:   credits,
		Timestamp: time.Now(),
		Metadata:  metadata,
	})
}

// GetEvents returns events for a user within a time range
func (m *Meter) GetEvents(userID string, since, until time.Time) []Event {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []Event
	for _, e := range m.events {
		if e.UserID != userID {
			continue
		}
		if !since.IsZero() && e.Timestamp.Before(since) {
			continue
		}
		if !until.IsZero() && e.Timestamp.After(until) {
			continue
		}
		result = append(result, e)
	}
	return result
}

// GetSummary returns usage summary for a user
func (m *Meter) GetSummary(userID string, since time.Time) *Summary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := &Summary{
		UserID: userID,
		Since:  since,
	}

	for _, e := range m.events {
		if e.UserID != userID {
			continue
		}
		if !since.IsZero() && e.Timestamp.Before(since) {
			continue
		}

		switch e.EventType {
		case EventUpload:
			s.UploadBytes += e.Bytes
			s.UploadCount++
		case EventDownload:
			s.DownloadBytes += e.Bytes
			s.DownloadCount++
		case EventDelete:
			s.DeleteBytes += e.Bytes
			s.DeleteCount++
		case EventGenImage, EventGenVideo, EventGenAudio:
			s.GenerationCredits += e.Credits
			s.GenerationCount++
		}
	}

	return s
}

// Summary holds usage summary
type Summary struct {
	UserID            string    `json:"user_id"`
	Since             time.Time `json:"since"`
	UploadBytes       int64     `json:"upload_bytes"`
	UploadCount       int64     `json:"upload_count"`
	DownloadBytes     int64     `json:"download_bytes"`
	DownloadCount     int64     `json:"download_count"`
	DeleteBytes       int64     `json:"delete_bytes"`
	DeleteCount       int64     `json:"delete_count"`
	GenerationCredits int       `json:"generation_credits"`
	GenerationCount   int64     `json:"generation_count"`
}
