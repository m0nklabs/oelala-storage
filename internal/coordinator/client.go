package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/m0nklabs/oelala-storage/internal/config"
	"github.com/m0nklabs/oelala-storage/internal/logging"
	"github.com/m0nklabs/oelala-storage/internal/storage"
)

// Client handles communication with the central coordinator
type Client struct {
	config      *config.CoordinatorConfig
	node        *config.NodeConfig
	storagePath string
	store       *storage.Store
	client      *http.Client
	ctx         context.Context
	cancel      context.CancelFunc
}

// HeartbeatPayload represents the data sent to the coordinator
type HeartbeatPayload struct {
	NodeID     string `json:"node_id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	PublicURL  string `json:"public_url"`
	TotalBytes int64  `json:"total_bytes"`
	UsedBytes  int64  `json:"used_bytes"`
	Status     string `json:"status"`
	Version    string `json:"version"`
}

// NewClient creates a new coordinator client
func NewClient(cfg *config.CoordinatorConfig, nodeCfg *config.NodeConfig, store *storage.Store, storagePath string) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		config:      cfg,
		node:        nodeCfg,
		storagePath: storagePath,
		store:       store,
		client:      &http.Client{Timeout: 10 * time.Second},
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start begins the background heartbeat routine
func (c *Client) Start(version string) {
	if !c.config.Enabled || c.config.URL == "" {
		logging.Info("Coordinator integration disabled")
		return
	}

	interval := time.Duration(c.config.HeartbeatSecs) * time.Second
	if interval < 10*time.Second {
		interval = 60 * time.Second // Sanity check
	}

	logging.Info("Starting coordinator heartbeat",
		zap.String("url", c.config.URL),
		zap.Duration("interval", interval))

	// Run initial heartbeat immediately
	c.sendHeartbeat(version)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-c.ctx.Done():
				logging.Info("Coordinator heartbeat stopped")
				return
			case <-ticker.C:
				c.sendHeartbeat(version)
			}
		}
	}()
}

// Stop stops the background routine
func (c *Client) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *Client) sendHeartbeat(version string) {
	// Gather stats
	usedBytes, totalBytes := getDiskSpace(c.storagePath)

	payload := HeartbeatPayload{
		NodeID:     c.node.ID,
		Name:       c.node.Name,
		Type:       c.node.Type,
		PublicURL:  c.node.PublicURL,
		TotalBytes: int64(totalBytes),
		UsedBytes:  int64(usedBytes),
		Status:     "online",
		Version:    version,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		logging.Error("Failed to marshal heartbeat payload", zap.Error(err))
		return
	}

	url := fmt.Sprintf("%s/api/storage-nodes/heartbeat", c.config.URL)
	req, err := http.NewRequestWithContext(c.ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		logging.Error("Failed to create heartbeat request", zap.Error(err))
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if c.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		logging.Error("Failed to send heartbeat to coordinator", zap.Error(err), zap.String("url", url))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		logging.Error("Coordinator returned error",
			zap.Int("status", resp.StatusCode),
			zap.String("response", string(body)))
		return
	}

	logging.Debug("Successfully sent heartbeat to coordinator")
}
