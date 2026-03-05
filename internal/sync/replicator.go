package sync

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	pb "github.com/m0nklabs/oelala-storage/api/proto"
	"github.com/m0nklabs/oelala-storage/internal/storage"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// internalBuckets are storage-internal directories that must NOT be synced between nodes.
var internalBuckets = map[string]bool{
	"apikeys":  true,
	"blobs":    true,
	"dedup":    true,
	"metadata": true,
}

// ReplicationState tracks sync state with a peer
type ReplicationState struct {
	PeerID        string    `json:"peer_id"`
	LastSync      time.Time `json:"last_sync"`
	ObjectsSynced int64     `json:"objects_synced"`
	BytesSynced   int64     `json:"bytes_synced"`
	Errors        int       `json:"errors"`
}

// Replicator handles sync between peers
type Replicator struct {
	store     *storage.Store
	peerID    string
	discovery *Discovery
	states    map[string]*ReplicationState
	mu        sync.RWMutex
	interval  time.Duration
	logger    *zap.Logger
}

// NewReplicator creates a new replication engine
func NewReplicator(store *storage.Store, peerID string, discovery *Discovery, logger *zap.Logger) *Replicator {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Replicator{
		store:     store,
		peerID:    peerID,
		discovery: discovery,
		states:    make(map[string]*ReplicationState),
		interval:  60 * time.Second,
		logger:    logger.Named("replicator"),
	}
}

// SetInterval sets the sync interval
func (r *Replicator) SetInterval(d time.Duration) {
	r.interval = d
}

// Start begins the replication loop
func (r *Replicator) Start(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.syncAll(ctx)
		}
	}
}

// SyncNow triggers immediate sync with all peers
func (r *Replicator) SyncNow(ctx context.Context) {
	r.syncAll(ctx)
}

// GetStates returns replication states
func (r *Replicator) GetStates() map[string]*ReplicationState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*ReplicationState)
	for k, v := range r.states {
		result[k] = v
	}
	return result
}

func (r *Replicator) syncAll(ctx context.Context) {
	peers := r.discovery.GetPeers()

	// Deduplicate peers by host:port (mDNS and static config may find the same node)
	seen := make(map[string]bool)
	var uniquePeers []*Peer
	for _, peer := range peers {
		addr := fmt.Sprintf("%s:%d", peer.Host, peer.Port)
		if seen[addr] || !peer.Available {
			continue
		}
		seen[addr] = true
		uniquePeers = append(uniquePeers, peer)
	}

	r.logger.Info("Sync cycle starting",
		zap.Int("peers_found", len(peers)), zap.Int("unique_peers", len(uniquePeers)))

	for _, peer := range uniquePeers {
		go r.syncWithPeer(ctx, peer)
	}
}

func (r *Replicator) syncWithPeer(ctx context.Context, peer *Peer) {
	r.mu.Lock()
	state, ok := r.states[peer.ID]
	if !ok {
		state = &ReplicationState{PeerID: peer.ID}
		r.states[peer.ID] = state
	}
	r.mu.Unlock()

	r.logger.Info("Starting sync with peer", zap.String("peer", peer.ID),
		zap.String("addr", fmt.Sprintf("%s:%d", peer.Host, peer.Port)))

	// Connect to peer
	addr := fmt.Sprintf("%s:%d", peer.Host, peer.Port)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		state.Errors++
		r.logger.Error("Failed to connect to peer", zap.String("peer", peer.ID), zap.Error(err))
		return
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewSyncServiceClient(conn)

	// Handshake
	_, err = client.Handshake(ctx, &pb.HandshakeRequest{
		Peer: &pb.PeerInfo{PeerId: r.peerID},
	})
	if err != nil {
		state.Errors++
		r.logger.Error("Handshake failed", zap.String("peer", peer.ID), zap.Error(err))
		return
	}

	// Pull objects from peer
	if err := r.pullFromPeer(ctx, client, state); err != nil {
		state.Errors++
		r.logger.Warn("Pull from peer failed", zap.String("peer", peer.ID), zap.Error(err))
	}

	// Push objects to peer
	if err := r.pushToPeer(ctx, client, state); err != nil {
		state.Errors++
		r.logger.Warn("Push to peer failed", zap.String("peer", peer.ID), zap.Error(err))
	}

	state.LastSync = time.Now()
	r.logger.Info("Sync completed with peer", zap.String("peer", peer.ID),
		zap.Int64("objects_synced", state.ObjectsSynced), zap.Int64("bytes_synced", state.BytesSynced))
}

func (r *Replicator) pullFromPeer(ctx context.Context, client pb.SyncServiceClient, state *ReplicationState) error {
	// Request all objects from all buckets (empty bucket = all content buckets)
	resp, err := client.ListObjects(ctx, &pb.ListObjectsRequest{
		SinceTimestamp: state.LastSync.Unix(),
		Limit:          1000,
	})
	if err != nil {
		return err
	}

	// Download objects we don't have
	for _, meta := range resp.Objects {
		// Skip internal buckets
		if internalBuckets[meta.Bucket] {
			continue
		}

		if r.store.Exists(meta.Bucket, meta.Key) {
			continue
		}

		r.logger.Debug("Pulling object", zap.String("bucket", meta.Bucket), zap.String("key", meta.Key),
			zap.Int64("size", meta.Size))

		if err := r.downloadObject(ctx, client, meta); err != nil {
			r.logger.Warn("Failed to download object",
				zap.String("bucket", meta.Bucket), zap.String("key", meta.Key), zap.Error(err))
			continue
		}

		state.ObjectsSynced++
		state.BytesSynced += meta.Size
	}

	return nil
}

func (r *Replicator) downloadObject(ctx context.Context, client pb.SyncServiceClient, meta *pb.ObjectMeta) error {
	stream, err := client.GetObject(ctx, &pb.GetObjectRequest{
		Bucket:       meta.Bucket,
		Key:          meta.Key,
		ExpectedHash: meta.Hash,
	})
	if err != nil {
		return err
	}

	var data []byte
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if d, ok := chunk.Payload.(*pb.ObjectChunk_Data); ok {
			data = append(data, d.Data...)
		}
	}

	// Store locally
	_, err = r.store.Put(meta.Bucket, meta.Key, bytes.NewReader(data))
	return err
}

func (r *Replicator) pushToPeer(ctx context.Context, client pb.SyncServiceClient, state *ReplicationState) error {
	// First, get the peer's inventory so we only push what's missing
	peerResp, err := client.ListObjects(ctx, &pb.ListObjectsRequest{
		Limit: 100000, // large limit to get full inventory
	})
	peerHashes := make(map[string]bool)
	if err == nil {
		for _, obj := range peerResp.Objects {
			peerHashes[obj.Hash] = true
		}
	}

	// Get all content buckets (skip internal ones)
	buckets, err := r.store.ListBuckets()
	if err != nil {
		return fmt.Errorf("list buckets: %w", err)
	}

	var pushed, skipped int
	for _, bucket := range buckets {
		if internalBuckets[bucket] {
			continue
		}

		objects, err := r.store.List(bucket, "")
		if err != nil {
			r.logger.Warn("Failed to list bucket for push", zap.String("bucket", bucket), zap.Error(err))
			continue
		}

		for _, obj := range objects {
			// Skip objects the peer already has
			if peerHashes[obj.Hash] {
				skipped++
				continue
			}

			if err := r.pushObject(ctx, client, obj, state); err != nil {
				r.logger.Warn("Failed to push object",
					zap.String("bucket", obj.Bucket), zap.String("key", obj.Key), zap.Error(err))
				continue
			}
			pushed++
		}
	}

	if pushed > 0 || skipped > 0 {
		r.logger.Info("Push summary", zap.Int("pushed", pushed), zap.Int("skipped_existing", skipped))
	}

	return nil
}

// pushObject pushes a single object to a peer
func (r *Replicator) pushObject(ctx context.Context, client pb.SyncServiceClient, obj *storage.Object, state *ReplicationState) error {
	stream, err := client.PushObject(ctx)
	if err != nil {
		return err
	}

	// Send metadata
	if err := stream.Send(&pb.ObjectChunk{
		Payload: &pb.ObjectChunk_Meta{
			Meta: &pb.ObjectMeta{
				Bucket:      obj.Bucket,
				Key:         obj.Key,
				Hash:        obj.Hash,
				Size:        obj.Size,
				ContentType: obj.ContentType,
			},
		},
	}); err != nil {
		return err
	}

	// Read and send data
	reader, _, err := r.store.Get(obj.Bucket, obj.Key)
	if err != nil {
		return err
	}

	buf := make([]byte, 64*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if err := stream.Send(&pb.ObjectChunk{
				Payload: &pb.ObjectChunk_Data{
					Data: buf[:n],
				},
			}); err != nil {
				_ = reader.Close()
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = reader.Close()
			return err
		}
	}
	_ = reader.Close()

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return err
	}

	if resp.Success {
		state.ObjectsSynced++
		state.BytesSynced += obj.Size
	}
	return nil
}
