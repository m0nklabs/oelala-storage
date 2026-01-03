package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/m0nklabs/oelala-storage/internal/api"
	"github.com/m0nklabs/oelala-storage/internal/config"
	"github.com/m0nklabs/oelala-storage/internal/storage"
	"github.com/spf13/cobra"
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
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("oelala-storage %s\n", version)
		fmt.Printf("Built: %s\n", buildTime)
	},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize configuration file",
	RunE: func(cmd *cobra.Command, args []string) error {
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
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("🚀 Starting oelala-storage node...")
		fmt.Printf("Version: %s\n", version)

		// Load config
		configPath := "oelala-storage.yaml"
		if cfgFile != "" {
			configPath = cfgFile
		}

		cfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		fmt.Printf("📁 Storage path: %s\n", cfg.Storage.Path)

		// Initialize storage
		store, err := storage.NewStore(cfg.Storage.Path, cfg.Storage.MaxSizeGB)
		if err != nil {
			return fmt.Errorf("failed to initialize storage: %w", err)
		}

		// Start HTTP server
		server := api.NewServer(store, cfg.API.HTTPPort)

		// Handle graceful shutdown
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-quit
			fmt.Println("\n🛑 Shutting down...")
			server.Stop()
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
