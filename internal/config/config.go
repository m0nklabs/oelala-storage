// Package config handles application configuration loading and management using Viper.
package config

import (
	"fmt"
	"os"

	"github.com/m0nklabs/oelala-storage/internal/webhook"
	"github.com/spf13/viper"
)

// Config represents the application configuration structure.
type Config struct {
	Node     NodeConfig        `mapstructure:"node"`
	Storage  StorageConfig     `mapstructure:"storage"`
	API      APIConfig         `mapstructure:"api"`
	Sync     SyncConfig        `mapstructure:"sync"`
	Security SecurityConfig    `mapstructure:"security"`
	Logging  LoggingConfig     `mapstructure:"logging"`
	Metrics  MetricsConfig     `mapstructure:"metrics"`
	TLS      TLSConfig         `mapstructure:"tls"`
	Webhooks webhook.Config    `mapstructure:"webhooks"`
}

// NodeConfig holds node-specific configuration.
type NodeConfig struct {
	ID   string `mapstructure:"id"`
	Name string `mapstructure:"name"`
	Type string `mapstructure:"type"` // primary, replica, edge, archive
}

// StorageConfig holds storage-specific configuration.
type StorageConfig struct {
	Path        string `mapstructure:"path"`
	MaxSizeGB   int    `mapstructure:"max_size_gb"`
	CacheSizeMB int    `mapstructure:"cache_size_mb"`
}

// APIConfig holds API server configuration.
type APIConfig struct {
	HTTPPort  int  `mapstructure:"http_port"`
	GRPCPort  int  `mapstructure:"grpc_port"`
	EnableTLS bool `mapstructure:"enable_tls"`
}

// LoggingConfig holds logging configuration.
type LoggingConfig struct {
	Level  string `mapstructure:"level"`  // debug, info, warn, error
	Format string `mapstructure:"format"` // json, console
}

// MetricsConfig holds metrics configuration.
type MetricsConfig struct {
	Enabled bool `mapstructure:"enabled"`
	Port    int  `mapstructure:"port"`
}

// TLSConfig holds TLS/SSL configuration.
type TLSConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
	AutoCert bool   `mapstructure:"auto_cert"` // Generate self-signed if missing
}

// SyncConfig holds peer synchronization configuration.
type SyncConfig struct {
	Enabled         bool       `mapstructure:"enabled"`
	Peers           []PeerInfo `mapstructure:"peers"`
	Strategy        string     `mapstructure:"strategy"` // realtime, scheduled, manual
	IntervalMinutes int        `mapstructure:"interval_minutes"`
}

// PeerInfo holds information about a sync peer.
type PeerInfo struct {
	URL  string `mapstructure:"url"`
	Type string `mapstructure:"type"`
}

// SecurityConfig holds security-related configuration.
type SecurityConfig struct {
	EncryptionAtRest bool        `mapstructure:"encryption_at_rest"`
	AuthTokens       []AuthToken `mapstructure:"auth_tokens"`
	AdminSecret      string      `mapstructure:"admin_secret"`   // Secret for /admin access
	SigningSecret    string      `mapstructure:"signing_secret"` // Secret for signed URLs
}

// AuthToken holds authentication token configuration.
type AuthToken struct {
	Name        string   `mapstructure:"name"`
	Token       string   `mapstructure:"token"`
	Permissions []string `mapstructure:"permissions"`
}

// Load loads configuration from the specified path or default locations.
func Load(path string) (*Config, error) {
	if path != "" {
		viper.SetConfigFile(path)
	} else {
		viper.SetConfigName("oelala-storage")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.config/oelala-storage")
		viper.AddConfigPath("/etc/oelala-storage")
	}

	// Environment variable support
	viper.SetEnvPrefix("OELALA_STORAGE")
	viper.AutomaticEnv()

	// Set defaults
	viper.SetDefault("node.id", "auto")
	viper.SetDefault("node.type", "primary")
	viper.SetDefault("storage.path", "./data")
	viper.SetDefault("storage.max_size_gb", 500)
	viper.SetDefault("storage.cache_size_mb", 2048)
	viper.SetDefault("api.http_port", 7990)
	viper.SetDefault("api.grpc_port", 7991)
	viper.SetDefault("sync.enabled", true)
	viper.SetDefault("sync.strategy", "realtime")
	viper.SetDefault("sync.interval_minutes", 15)
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "console")
	viper.SetDefault("metrics.enabled", true)
	viper.SetDefault("metrics.port", 7992)
	viper.SetDefault("tls.enabled", false)
	viper.SetDefault("tls.auto_cert", true)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Config file not found, use defaults
			fmt.Fprintln(os.Stderr, "⚠️  No config file found, using defaults")
		} else {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}
