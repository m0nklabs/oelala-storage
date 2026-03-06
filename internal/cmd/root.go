// Package cmd provides the command-line interface for oelala-storage.
package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/m0nklabs/oelala-storage/internal/api"
	"github.com/m0nklabs/oelala-storage/internal/apikeys"
	"github.com/m0nklabs/oelala-storage/internal/auth"
	"github.com/m0nklabs/oelala-storage/internal/config"
	"github.com/m0nklabs/oelala-storage/internal/coordinator"
	"github.com/m0nklabs/oelala-storage/internal/dedup"
	"github.com/m0nklabs/oelala-storage/internal/gc"
	"github.com/m0nklabs/oelala-storage/internal/logging"
	"github.com/m0nklabs/oelala-storage/internal/metadata"
	"github.com/m0nklabs/oelala-storage/internal/metrics"
	"github.com/m0nklabs/oelala-storage/internal/storage"
	"github.com/m0nklabs/oelala-storage/internal/sync"
	internaltls "github.com/m0nklabs/oelala-storage/internal/tls"
	"github.com/m0nklabs/oelala-storage/internal/webhook"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	version   string
	buildTime string
	cfgFile   string
)

// parseHostPort splits a "host:port" string into host and port int.
func parseHostPort(addr string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port %q: %w", portStr, err)
	}
	return host, port, nil
}

// Execute runs the root command with the provided version and build time.
func Execute(v, bt string) error {
	version = v
	buildTime = bt
	return rootCmd.Execute()
}

var rootCmd = &cobra.Command{
	Use:   "oelala-storage",
	Short: "Distributed storage service for Oelala",
	Long: `oelala-storage is a lightweight, distributed storage node that provides:
  - S3-compatible API
  - P2P sync between nodes
  - Content-addressed deduplication
  - Cross-platform support (Windows, Linux, Android)`,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Printf("oelala-storage %s\n", version)
		fmt.Printf("Built: %s\n", buildTime)
	},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize configuration file",
	RunE: func(_ *cobra.Command, _ []string) error {
		configPath := "oelala-storage.yaml"
		if cfgFile != "" {
			configPath = cfgFile
		}

		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("config file already exists: %s", configPath)
		}

		defaultConfig := `# oelala-storage configuration
node:
  id: "auto"
  name: "My Storage Node"
  type: primary  # primary, replica, edge, archive

storage:
  path: "./data"
  max_size_gb: 500
  cache_size_mb: 2048

api:
  http_port: 7999
  grpc_port: 7998
  enable_tls: false

sync:
  enabled: true
  peers: []
  strategy: realtime
  interval_minutes: 15

security:
  encryption_at_rest: false
  auth_tokens:
    - name: "default"
      token: ""
      permissions: ["read", "write", "delete"]
`
		if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}

		fmt.Printf("✅ Created config file: %s\n", configPath)
		fmt.Println("Edit the file to configure your node, then run: oelala-storage serve")
		return nil
	},
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the storage node",
	RunE: func(_ *cobra.Command, _ []string) error {
		// Load config first
		configPath := "oelala-storage.yaml"
		if cfgFile != "" {
			configPath = cfgFile
		}

		cfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Initialize structured logging
		logCfg := logging.Config{
			Level:    cfg.Logging.Level,
			Encoding: cfg.Logging.Format,
		}
		if err := logging.Init(logCfg); err != nil {
			return fmt.Errorf("failed to init logging: %w", err)
		}
		logging.Info("Starting oelala-storage node",
			zap.String("version", version),
			zap.String("storage_path", cfg.Storage.Path),
		)

		// Initialize metrics
		if cfg.Metrics.Enabled {
			metrics.Init()
			go func() {
				addr := fmt.Sprintf(":%d", cfg.Metrics.Port)
				logging.Info("Metrics server starting", zap.String("addr", addr))
				http.Handle("/metrics", metrics.Handler())
				if err := http.ListenAndServe(addr, nil); err != nil {
					logging.Error("Metrics server failed", zap.Error(err))
				}
			}()
		}

		// Initialize storage
		store, err := storage.NewStore(cfg.Storage.Path, cfg.Storage.MaxSizeGB)
		if err != nil {
			return fmt.Errorf("failed to initialize storage: %w", err)
		}

		// Initialize metadata store for object metadata and expiration tracking
		metadataPath := filepath.Join(cfg.Storage.Path, "metadata")
		metadataStore, err := metadata.NewStore(metadata.DefaultOptions(metadataPath))
		if err != nil {
			return fmt.Errorf("failed to initialize metadata store: %w", err)
		}
		defer metadataStore.Close()
		logging.Info("Metadata store initialized", zap.String("path", metadataPath))

		// Initialize deduplication store
		dedupBlobPath := filepath.Join(cfg.Storage.Path, "blobs")
		dedupDBPath := filepath.Join(cfg.Storage.Path, "dedup")
		dedupStore, err := dedup.NewStore(dedup.Options{
			BlobPath:   dedupBlobPath,
			DBPath:     dedupDBPath,
			SyncWrites: false,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize dedup store: %w", err)
		}
		defer dedupStore.Close()
		logging.Info("Dedup store initialized", zap.String("blob_path", dedupBlobPath))

		// Setup TLS if enabled
		var tlsConfig *tls.Config
		if cfg.TLS.Enabled || cfg.API.EnableTLS {
			tlsCfg := internaltls.Config{
				Enabled:  true,
				CertFile: cfg.TLS.CertFile,
				KeyFile:  cfg.TLS.KeyFile,
				AutoCert: cfg.TLS.AutoCert,
			}
			tlsConfig, err = internaltls.LoadOrGenerate(tlsCfg)
			if err != nil {
				return fmt.Errorf("failed to setup TLS: %w", err)
			}
			logging.Info("TLS enabled")
		}

		// Start gRPC sync server
		var syncServer *sync.Server
		var discovery *sync.Discovery
		var cancelReplicator context.CancelFunc
		if cfg.Sync.Enabled {
			syncServer = sync.NewServer(store, cfg.Node.ID, cfg.API.GRPCPort, version)
			if err := syncServer.Start(); err != nil {
				return fmt.Errorf("failed to start sync server: %w", err)
			}
			logging.Info("gRPC sync server started", zap.Int("port", cfg.API.GRPCPort))

			// Start peer discovery
			discovery = sync.NewDiscovery(cfg.Node.ID, cfg.API.GRPCPort, version)
			ctx, cancel := context.WithCancel(context.Background())
			cancelReplicator = cancel
			if err := discovery.Start(ctx); err != nil {
				logging.Warn("Peer discovery failed to start", zap.Error(err))
			} else {
				logging.Info("mDNS peer discovery started")
			}

			// Add static peers from config
			for _, peer := range cfg.Sync.Peers {
				host, portStr, err := parseHostPort(peer.URL)
				if err != nil {
					logging.Warn("Invalid static peer URL", zap.String("url", peer.URL), zap.Error(err))
					continue
				}
				discovery.AddStaticPeer(host, portStr)
				logging.Info("Static peer added", zap.String("host", host), zap.Int("port", portStr))
			}

			// Start replicator
			replicator := sync.NewReplicator(store, cfg.Node.ID, discovery, logging.Logger)
			interval := time.Duration(cfg.Sync.IntervalMinutes) * time.Minute
			if interval == 0 {
				interval = 15 * time.Minute
			}
			replicator.SetInterval(interval)
			go replicator.Start(ctx)
			logging.Info("Replication engine started", zap.Duration("interval", interval))
		}

		// Start HTTP server
		serverOpts := []api.ServerOption{}
		if tlsConfig != nil {
			serverOpts = append(serverOpts, api.WithTLS(tlsConfig))
		}
		if cfg.Metrics.Enabled {
			serverOpts = append(serverOpts, api.WithMetrics())
		}

		// Add metadata store for expiration tracking
		serverOpts = append(serverOpts, api.WithMetadataStore(metadataStore))

		// Add dedup store for deduplication stats
		serverOpts = append(serverOpts, api.WithDedupStore(dedupStore))

		// Initialize API keys store for dynamic key management
		keyStorePath := filepath.Join(cfg.Storage.Path, "apikeys")
		keyStore, err := apikeys.NewStore(keyStorePath)
		if err != nil {
			return fmt.Errorf("failed to initialize apikeys store: %w", err)
		}
		defer keyStore.Close()
		logging.Info("API keys store initialized", zap.String("path", keyStorePath))

		// Setup admin server for API key management UI
		adminServer := api.NewAdminServer(keyStore, cfg.Security.AdminSecret)
		serverOpts = append(serverOpts, api.WithAdminServer(adminServer))
		if cfg.Security.AdminSecret != "" {
			logging.Info("Admin UI enabled at /admin/ (protected)")
		} else {
			logging.Warn("Admin UI enabled at /admin/ (NO PASSWORD SET - use security.admin_secret)")
		}

		// Configure authentication
		authCfg := auth.Config{
			APIKeys:       make(map[string]*auth.UserContext),
			SkipPaths:     []string{"/health", "/status", "/metrics", "/admin", "/admin/", "/s/"},
			SigningSecret: cfg.Security.SigningSecret, // For signed URL verification
			// Dynamic key validation via apikeys store
			TokenValidator: func(token string) *auth.UserContext {
				// Try dynamic key store first
				if key, err := keyStore.ValidateKey(token); err == nil {
					return &auth.UserContext{
						UserID: key.Name,
						Roles:  key.Permissions,
					}
				}
				return nil
			},
		}

		// Add static tokens from config
		for _, token := range cfg.Security.AuthTokens {
			if token.Token != "" {
				authCfg.APIKeys[token.Token] = &auth.UserContext{
					UserID: token.Name,
					Roles:  token.Permissions,
				}
				logging.Info("Static API key configured", zap.String("name", token.Name))
			}
		}

		serverOpts = append(serverOpts, api.WithAuth(authCfg))
		logging.Info("Authentication enabled",
			zap.Int("static_keys", len(authCfg.APIKeys)),
			zap.Bool("signed_urls", cfg.Security.SigningSecret != ""))

		// Enable signed URLs if signing secret is configured
		if cfg.Security.SigningSecret != "" {
			// Determine base URL for signed URLs
			baseURL := fmt.Sprintf("http://localhost:%d", cfg.API.HTTPPort)
			if tlsConfig != nil {
				baseURL = fmt.Sprintf("https://localhost:%d", cfg.API.HTTPPort)
			}
			serverOpts = append(serverOpts, api.WithSigningSecret(cfg.Security.SigningSecret, baseURL))
			logging.Info("Signed URLs enabled")
		}

		// Initialize webhook dispatcher
		var webhookDispatcher *webhook.Dispatcher
		if cfg.Webhooks.Enabled {
			webhookDispatcher = webhook.NewDispatcher(cfg.Webhooks, cfg.Node.ID)
			serverOpts = append(serverOpts, api.WithWebhooks(webhookDispatcher))
			logging.Info("Webhooks enabled", zap.Int("targets", len(cfg.Webhooks.Targets)))
		}

		server := api.NewServer(store, cfg.API.HTTPPort, serverOpts...)

		// Start coordinator client
		coordClient := coordinator.NewClient(&cfg.Coordinator, &cfg.Node, store, cfg.Storage.Path)
		coordClient.Start(version)

		// Start garbage collector in background
		gcConfig := gc.DefaultConfig()
		collector := gc.NewCollector(store, metadataStore, gcConfig)
		gcCtx, gcCancel := context.WithCancel(context.Background())
		go collector.Start(gcCtx)

		// Add GC collector to server for API access
		server.SetGCFunctions(
			func() interface{} { return collector.RunOnce() },
			func() interface{} { return collector.GetStats() },
		)

		// Handle graceful shutdown
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-quit
			logging.Info("Shutting down...")
			gcCancel() // Stop GC
			if webhookDispatcher != nil {
				webhookDispatcher.Stop()
			}
			if cancelReplicator != nil {
				cancelReplicator()
			}
			if syncServer != nil {
				syncServer.Stop()
			}
			if coordClient != nil {
				coordClient.Stop()
			}
			if discovery != nil {
				_ = discovery.Stop()
			}
			_ = server.Stop()
		}()

		return server.Start()
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: oelala-storage.yaml)")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(serveCmd)
}
