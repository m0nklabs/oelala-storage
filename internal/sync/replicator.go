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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

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
}

// NewReplicator creates a new replication engine
func NewReplicator(store *storage.Store, peerID string, discovery *Discovery) *Replicator {
	return &Replicator{
		store:     store,
		peerID:    peerID,
		discovery: discovery,
		states:    make(map[string]*ReplicationState),
		interval:  60 * time.Second,
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
	for _, peer := range peers {
		if peer.Available {
			go r.syncWithPeer(ctx, peer)
		}
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

	// Connect to peer
	addr := fmt.Sprintf("%s:%d", peer.Host, peer.Port)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		state.Errors++
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
		return
	}

	// Pull objects from peer
	if err := r.pullFromPeer(ctx, client, state); err != nil {
		state.Errors++
		return
	}

	// Push objects to peer
	if err := r.pushToPeer(ctx, client, state); err != nil {
		state.Errors++
		return
	}

	state.LastSync = time.Now()
}

func (r *Replicator) pullFromPeer(ctx context.Context, client pb.SyncServiceClient, state *ReplicationState) error {
	// Get list of objects from peer
	resp, err := client.ListObjects(ctx, &pb.ListObjectsRequest{
		SinceTimestamp: state.LastSync.Unix(),
		Limit:          100,
	})
	if err != nil {
		return err
	}

	// Download objects we don't have
	for _, meta := range resp.Objects {
		if r.store.Exists(meta.Bucket, meta.Key) {
			continue
		}

		if err := r.downloadObject(ctx, client, meta); err != nil {
			return err
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
	// Get our objects
	objects, err := r.store.List("default", "")
	if err != nil {
		return err
	}

	for _, obj := range objects {
		// Skip if peer already has it (we'd need to track this better)
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
	}

	return nil
}
