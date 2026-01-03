package quota

import (
	"fmt"
	"sync"
)

// Tier represents a subscription tier
type Tier struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	QuotaBytes    int64  `json:"quota_bytes"`
	RetentionDays int    `json:"retention_days"` // -1 for unlimited
}

// Predefined tiers
var (
	FreeTier = Tier{
		ID:            "free",
		Name:          "Free",
		QuotaBytes:    2 * 1024 * 1024 * 1024, // 2GB
		RetentionDays: 30,
	}
	CreatorTier = Tier{
		ID:            "creator",
		Name:          "Creator",
		QuotaBytes:    50 * 1024 * 1024 * 1024, // 50GB
		RetentionDays: -1,
	}
	ProTier = Tier{
		ID:            "pro",
		Name:          "Pro",
		QuotaBytes:    200 * 1024 * 1024 * 1024, // 200GB
		RetentionDays: -1,
	}
	StudioTier = Tier{
		ID:            "studio",
		Name:          "Studio",
		QuotaBytes:    1024 * 1024 * 1024 * 1024, // 1TB
		RetentionDays: -1,
	}
)

// GetTier returns tier by ID
func GetTier(id string) *Tier {
	switch id {
	case "creator":
		return &CreatorTier
	case "pro":
		return &ProTier
	case "studio":
		return &StudioTier
	default:
		return &FreeTier
	}
}

// Usage tracks storage usage for a user
type Usage struct {
	UserID       string `json:"user_id"`
	TierID       string `json:"tier_id"`
	UsedBytes    int64  `json:"used_bytes"`
	ObjectCount  int64  `json:"object_count"`
	QuotaBytes   int64  `json:"quota_bytes"`
}

// Tracker tracks storage quotas and usage
type Tracker struct {
	usage map[string]*Usage
	mu    sync.RWMutex
}

// NewTracker creates a new quota tracker
func NewTracker() *Tracker {
	return &Tracker{
		usage: make(map[string]*Usage),
	}
}

// GetUsage returns usage for a user
func (t *Tracker) GetUsage(userID string) *Usage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if u, ok := t.usage[userID]; ok {
		return u
	}
	return nil
}

// SetTier sets the tier for a user
func (t *Tracker) SetTier(userID, tierID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	tier := GetTier(tierID)
	if u, ok := t.usage[userID]; ok {
		u.TierID = tierID
		u.QuotaBytes = tier.QuotaBytes
	} else {
		t.usage[userID] = &Usage{
			UserID:     userID,
			TierID:     tierID,
			QuotaBytes: tier.QuotaBytes,
		}
	}
}

// AddUsage records storage usage
func (t *Tracker) AddUsage(userID string, bytes int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	u, ok := t.usage[userID]
	if !ok {
		tier := GetTier("free")
		u = &Usage{
			UserID:     userID,
			TierID:     "free",
			QuotaBytes: tier.QuotaBytes,
		}
		t.usage[userID] = u
	}

	// Check quota
	if u.UsedBytes+bytes > u.QuotaBytes {
		return &QuotaExceededError{
			UserID:    userID,
			Used:      u.UsedBytes,
			Requested: bytes,
			Quota:     u.QuotaBytes,
		}
	}

	u.UsedBytes += bytes
	u.ObjectCount++
	return nil
}

// RemoveUsage removes storage usage
func (t *Tracker) RemoveUsage(userID string, bytes int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if u, ok := t.usage[userID]; ok {
		u.UsedBytes -= bytes
		if u.UsedBytes < 0 {
			u.UsedBytes = 0
		}
		u.ObjectCount--
		if u.ObjectCount < 0 {
			u.ObjectCount = 0
		}
	}
}

// CheckQuota checks if user has enough quota
func (t *Tracker) CheckQuota(userID string, bytes int64) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	u, ok := t.usage[userID]
	if !ok {
		tier := GetTier("free")
		if bytes > tier.QuotaBytes {
			return &QuotaExceededError{
				UserID:    userID,
				Used:      0,
				Requested: bytes,
				Quota:     tier.QuotaBytes,
			}
		}
		return nil
	}

	if u.UsedBytes+bytes > u.QuotaBytes {
		return &QuotaExceededError{
			UserID:    userID,
			Used:      u.UsedBytes,
			Requested: bytes,
			Quota:     u.QuotaBytes,
		}
	}

	return nil
}

// GetAllUsage returns all user usage data
func (t *Tracker) GetAllUsage() map[string]*Usage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string]*Usage)
	for k, v := range t.usage {
		result[k] = v
	}
	return result
}

// QuotaExceededError indicates quota was exceeded
type QuotaExceededError struct {
	UserID    string
	Used      int64
	Requested int64
	Quota     int64
}

func (e *QuotaExceededError) Error() string {
	return fmt.Sprintf("quota exceeded: user %s used %d bytes, requested %d, quota %d",
		e.UserID, e.Used, e.Requested, e.Quota)
}
