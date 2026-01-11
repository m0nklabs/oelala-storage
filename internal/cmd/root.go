// Package cmd provides the command-line interface for oelala-storage.
package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/m0nklabs/oelala-storage/internal/api"
	"github.com/m0nklabs/oelala-storage/internal/config"
	"github.com/m0nklabs/oelala-storage/internal/logging"
	"github.com/m0nklabs/oelala-storage/internal/metrics"
	"github.com/m0nklabs/oelala-storage/internal/storage"
	"github.com/m0nklabs/oelala-storage/internal/sync"
	internaltls "github.com/m0nklabs/oelala-storage/internal/tls"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	version   string
	buildTime string
	cfgFile   string
)

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
	Run: func(_ *cobra.Command, args []string) {
		fmt.Printf("oelala-storage %s\n", version)
		fmt.Printf("Built: %s\n", buildTime)
	},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize configuration file",
	RunE: func(_ *cobra.Command, args []string) error {
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
	RunE: func(_ *cobra.Command, args []string) error {
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

			// Start replicator
			replicator := sync.NewReplicator(store, cfg.Node.ID, discovery)
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

		server := api.NewServer(store, cfg.API.HTTPPort, serverOpts...)

		// Handle graceful shutdown
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-quit
			logging.Info("Shutting down...")
			if cancelReplicator != nil {
				cancelReplicator()
			}
			if syncServer != nil {
				syncServer.Stop()
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
