// Package metering — persistent metering via BadgerDB.
// Events are written to BadgerDB on Record() and survive restarts.
// The in-memory ring buffer is used for fast queries; on startup it is
// populated from the last N events in BadgerDB.
package metering

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	badger "github.com/dgraph-io/badger/v4"
)

const (
	// meterPrefix is the BadgerDB key prefix for metering events.
	meterPrefix = "meter:"
	// defaultWarmupLimit is how many events to load from disk on startup.
	defaultWarmupLimit = 10000
)

// PersistentMeter wraps Meter and persists events to BadgerDB.
type PersistentMeter struct {
	*Meter
	db *badger.DB
}

// NewPersistentMeter creates a new persistent meter backed by BadgerDB.
// The db instance should be the shared BadgerDB already used for metadata/dedup.
func NewPersistentMeter(db *badger.DB, maxEvents int) *PersistentMeter {
	pm := &PersistentMeter{
		Meter: NewMeter(maxEvents),
		db:    db,
	}

	// Warm up the in-memory buffer from disk
	if err := pm.warmup(); err != nil {
		log.Printf("[metering] WARNING: failed to load events from disk: %v", err)
	}

	return pm
}

// Record persists the event to BadgerDB and then records in-memory.
func (pm *PersistentMeter) Record(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Generate a key: meter:<timestamp_ns>:<event_type>
	key := fmt.Sprintf("%s%020d:%s", meterPrefix, event.Timestamp.UnixNano(), event.EventType)

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[metering] ERROR: failed to marshal event: %v", err)
		pm.Meter.Record(event)
		return
	}

	if err := pm.db.Update(func(txn *badger.Txn) error {
		// Set with 90-day TTL — events older than that are auto-purged
		e := badger.NewEntry([]byte(key), data).WithTTL(90 * 24 * time.Hour)
		return txn.SetEntry(e)
	}); err != nil {
		log.Printf("[metering] ERROR: failed to persist event: %v", err)
	}

	// Also record in-memory for fast queries
	pm.Meter.Record(event)
}

// warmup loads the most recent events from BadgerDB into the in-memory buffer.
func (pm *PersistentMeter) warmup() error {
	var events []Event

	err := pm.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(meterPrefix)
		opts.Reverse = true // newest first

		it := txn.NewIterator(opts)
		defer it.Close()

		count := 0
		// Seek to the end of the prefix range
		seekKey := []byte(meterPrefix + "\xff")
		for it.Seek(seekKey); it.ValidForPrefix([]byte(meterPrefix)); it.Next() {
			if count >= defaultWarmupLimit {
				break
			}

			item := it.Item()
			var event Event
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &event)
			}); err != nil {
				continue // skip corrupted entries
			}
			events = append(events, event)
			count++
		}
		return nil
	})

	if err != nil {
		return err
	}

	// Reverse to chronological order and insert into meter
	for i := len(events) - 1; i >= 0; i-- {
		pm.Meter.Record(events[i])
	}

	if len(events) > 0 {
		log.Printf("[metering] Loaded %d events from disk", len(events))
	}

	return nil
}

// RecordUpload persists and records an upload event.
func (pm *PersistentMeter) RecordUpload(userID, bucket, key, contentType string, bytes int64) {
	pm.Record(Event{
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

// RecordDownload persists and records a download event.
func (pm *PersistentMeter) RecordDownload(userID, bucket, key string, bytes int64) {
	pm.Record(Event{
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

// RecordDelete persists and records a delete event.
func (pm *PersistentMeter) RecordDelete(userID, bucket, key string, bytes int64) {
	pm.Record(Event{
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
