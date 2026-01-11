package sync

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
)

const (
	serviceName = "_oelala-storage._tcp"
	domain      = "local."
)

// Peer represents a discovered storage peer
type Peer struct {
	ID        string    `json:"id"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Version   string    `json:"version"`
	LastSeen  time.Time `json:"last_seen"`
	Available bool      `json:"available"`
}

// Discovery handles mDNS peer discovery
type Discovery struct {
	peerID   string
	port     int
	version  string
	server   *mdns.Server
	peers    map[string]*Peer
	mu       sync.RWMutex
	onChange func(*Peer, bool) // callback when peer added/removed
}

// NewDiscovery creates a new peer discovery service
func NewDiscovery(peerID string, port int, version string) *Discovery {
	return &Discovery{
		peerID:  peerID,
		port:    port,
		version: version,
		peers:   make(map[string]*Peer),
	}
}

// OnChange sets callback for peer changes
func (d *Discovery) OnChange(fn func(*Peer, bool)) {
	d.onChange = fn
}

// Start begins advertising and discovering peers
func (d *Discovery) Start(ctx context.Context) error {
	// Start advertising ourselves
	if err := d.advertise(); err != nil {
		return fmt.Errorf("failed to advertise: %w", err)
	}

	// Start discovery loop
	go d.discoveryLoop(ctx)

	return nil
}

// Stop stops the discovery service
func (d *Discovery) Stop() error {
	if d.server != nil {
		_ = d.server.Shutdown()
	}
	return nil
}

// GetPeers returns all known peers
func (d *Discovery) GetPeers() []*Peer {
	d.mu.RLock()
	defer d.mu.RUnlock()

	peers := make([]*Peer, 0, len(d.peers))
	for _, p := range d.peers {
		peers = append(peers, p)
	}
	return peers
}

// GetPeer returns a specific peer by ID
func (d *Discovery) GetPeer(id string) *Peer {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.peers[id]
}

func (d *Discovery) advertise() error {
	// Get local IPs
	ips, err := getLocalIPs()
	if err != nil {
		return err
	}

	// Create mDNS service
	info := []string{
		fmt.Sprintf("id=%s", d.peerID),
		fmt.Sprintf("version=%s", d.version),
	}

	service, err := mdns.NewMDNSService(
		d.peerID,
		serviceName,
		domain,
		"",
		d.port,
		ips,
		info,
	)
	if err != nil {
		return err
	}

	// Create mDNS server
	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		return err
	}

	d.server = server
	return nil
}

func (d *Discovery) discoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Initial scan
	d.scan()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.scan()
			d.pruneStale()
		}
	}
}

func (d *Discovery) scan() {
	entriesCh := make(chan *mdns.ServiceEntry, 10)
	go func() {
		for entry := range entriesCh {
			d.handleEntry(entry)
		}
	}()

	// Lookup peers
	params := &mdns.QueryParam{
		Service:             serviceName,
		Domain:              domain,
		Timeout:             5 * time.Second,
		Entries:             entriesCh,
		WantUnicastResponse: true,
	}

	_ = mdns.Query(params)
	close(entriesCh)
}

func (d *Discovery) handleEntry(entry *mdns.ServiceEntry) {
	// Parse info
	var peerID, version string
	for _, field := range entry.InfoFields {
		if len(field) > 3 && field[:3] == "id=" {
			peerID = field[3:]
		}
		if len(field) > 8 && field[:8] == "version=" {
			version = field[8:]
		}
	}

	// Skip ourselves
	if peerID == d.peerID || peerID == "" {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	_, exists := d.peers[peerID]
	peer := &Peer{
		ID:        peerID,
		Host:      entry.AddrV4.String(),
		Port:      entry.Port,
		Version:   version,
		LastSeen:  time.Now(),
		Available: true,
	}

	if entry.AddrV4 == nil && entry.AddrV6 != nil {
		peer.Host = entry.AddrV6.String()
	}

	d.peers[peerID] = peer

	if !exists && d.onChange != nil {
		d.onChange(peer, true)
	}
}

func (d *Discovery) pruneStale() {
	d.mu.Lock()
	defer d.mu.Unlock()

	staleThreshold := time.Now().Add(-2 * time.Minute)
	for id, peer := range d.peers {
		if peer.LastSeen.Before(staleThreshold) {
			delete(d.peers, id)
			if d.onChange != nil {
				d.onChange(peer, false)
			}
		}
	}
}

func getLocalIPs() ([]net.IP, error) {
	var ips []net.IP

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP)
			}
		}
	}

	return ips, nil
}
