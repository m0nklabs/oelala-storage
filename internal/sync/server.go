// Package sync provides peer-to-peer synchronization capabilities for distributed storage.
package sync

import (
	"context"
	"fmt"
	"io"
	"net"

	pb "github.com/m0nklabs/oelala-storage/api/proto"
	"github.com/m0nklabs/oelala-storage/internal/storage"
	"google.golang.org/grpc"
)

// Server implements the gRPC SyncService
type Server struct {
	pb.UnimplementedSyncServiceServer
	store    *storage.Store
	peerInfo *pb.PeerInfo
	grpcSrv  *grpc.Server
	port     int
}

// NewServer creates a new sync server
func NewServer(store *storage.Store, peerID string, port int, version string) *Server {
	return &Server{
		store: store,
		port:  port,
		peerInfo: &pb.PeerInfo{
			PeerId:  peerID,
			Port:    int32(port),
			Version: version,
		},
	}
}

// Start begins listening for gRPC connections
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.grpcSrv = grpc.NewServer()
	pb.RegisterSyncServiceServer(s.grpcSrv, s)

	go func() { _ = s.grpcSrv.Serve(lis) }()
	return nil
}

// Stop gracefully stops the server
func (s *Server) Stop() {
	if s.grpcSrv != nil {
		s.grpcSrv.GracefulStop()
	}
}

// Handshake implements SyncService.Handshake
func (s *Server) Handshake(_ context.Context, _ *pb.HandshakeRequest) (*pb.HandshakeResponse, error) {
	return &pb.HandshakeResponse{
		Peer:            s.peerInfo,
		Accepted:        true,
		EnabledFeatures: []string{"sync", "push", "pull"},
	}, nil
}

// ListObjects implements SyncService.ListObjects
func (s *Server) ListObjects(_ context.Context, req *pb.ListObjectsRequest) (*pb.ListObjectsResponse, error) {
	bucket := req.Bucket
	if bucket == "" {
		bucket = "default"
	}

	objects, err := s.store.List(bucket, req.Prefix)
	if err != nil {
		return nil, err
	}

	// Convert to proto
	var pbObjects []*pb.ObjectMeta
	excludeSet := make(map[string]bool)
	for _, h := range req.ExcludeHashes {
		excludeSet[h] = true
	}

	for _, obj := range objects {
		// Skip if we already have it
		if excludeSet[obj.Hash] {
			continue
		}

		// Filter by timestamp
		if req.SinceTimestamp > 0 && obj.ModifiedAt.Unix() < req.SinceTimestamp {
			continue
		}

		pbObjects = append(pbObjects, &pb.ObjectMeta{
			Bucket:      obj.Bucket,
			Key:         obj.Key,
			Hash:        obj.Hash,
			Size:        obj.Size,
			ContentType: obj.ContentType,
			CreatedAt:   obj.CreatedAt.Unix(),
			ModifiedAt:  obj.ModifiedAt.Unix(),
		})

		if req.Limit > 0 && len(pbObjects) >= int(req.Limit) {
			break
		}
	}

	return &pb.ListObjectsResponse{
		Objects: pbObjects,
		HasMore: len(pbObjects) == int(req.Limit),
	}, nil
}

// GetObject implements SyncService.GetObject
func (s *Server) GetObject(req *pb.GetObjectRequest, stream pb.SyncService_GetObjectServer) error {
	reader, obj, err := s.store.Get(req.Bucket, req.Key)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	// Verify hash if requested
	if req.ExpectedHash != "" && obj.Hash != req.ExpectedHash {
		return fmt.Errorf("hash mismatch: expected %s, got %s", req.ExpectedHash, obj.Hash)
	}

	// Send metadata first
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

	// Stream data in chunks
	buf := make([]byte, 64*1024) // 64KB chunks
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if err := stream.Send(&pb.ObjectChunk{
				Payload: &pb.ObjectChunk_Data{
					Data: buf[:n],
				},
			}); err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// PushObject implements SyncService.PushObject
func (s *Server) PushObject(stream pb.SyncService_PushObjectServer) error {
	var meta *pb.ObjectMeta
	var data []byte

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		switch p := chunk.Payload.(type) {
		case *pb.ObjectChunk_Meta:
			meta = p.Meta
		case *pb.ObjectChunk_Data:
			data = append(data, p.Data...)
		}
	}

	if meta == nil {
		return fmt.Errorf("no metadata received")
	}

	// Store the object
	obj, err := s.store.Put(meta.Bucket, meta.Key, newBytesReader(data))
	if err != nil {
		return stream.SendAndClose(&pb.PushObjectResponse{
			Success: false,
			Error:   err.Error(),
		})
	}

	return stream.SendAndClose(&pb.PushObjectResponse{
		Success: true,
		Hash:    obj.Hash,
	})
}

// SyncStatus implements SyncService.SyncStatus
func (s *Server) SyncStatus(_ context.Context, _ *pb.SyncStatusRequest) (*pb.SyncStatusResponse, error) {
	return &pb.SyncStatusResponse{
		State: pb.SyncState_SYNC_STATE_IDLE,
	}, nil
}

// bytesReader implements io.Reader for []byte
type bytesReader struct {
	data []byte
	pos  int
}

func newBytesReader(data []byte) *bytesReader {
	return &bytesReader{data: data}
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
