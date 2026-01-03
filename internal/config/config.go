package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

type Config struct {
	Node     NodeConfig     `mapstructure:"node"`
	Storage  StorageConfig  `mapstructure:"storage"`
	API      APIConfig      `mapstructure:"api"`
	Sync     SyncConfig     `mapstructure:"sync"`
	Security SecurityConfig `mapstructure:"security"`
}

type NodeConfig struct {
	ID   string `mapstructure:"id"`
	Name string `mapstructure:"name"`
	Type string `mapstructure:"type"` // primary, replica, edge, archive
}

type StorageConfig struct {
	Path        string `mapstructure:"path"`
	MaxSizeGB   int    `mapstructure:"max_size_gb"`
	CacheSizeMB int    `mapstructure:"cache_size_mb"`
}

type APIConfig struct {
	HTTPPort  int  `mapstructure:"http_port"`
	GRPCPort  int  `mapstructure:"grpc_port"`
	EnableTLS bool `mapstructure:"enable_tls"`
}

type SyncConfig struct {
	Enabled         bool       `mapstructure:"enabled"`
	Peers           []PeerInfo `mapstructure:"peers"`
	Strategy        string     `mapstructure:"strategy"` // realtime, scheduled, manual
	IntervalMinutes int        `mapstructure:"interval_minutes"`
}

type PeerInfo struct {
	URL  string `mapstructure:"url"`
	Type string `mapstructure:"type"`
}

type SecurityConfig struct {
	EncryptionAtRest bool        `mapstructure:"encryption_at_rest"`
	AuthTokens       []AuthToken `mapstructure:"auth_tokens"`
}

type AuthToken struct {
	Name        string   `mapstructure:"name"`
	Token       string   `mapstructure:"token"`
	Permissions []string `mapstructure:"permissions"`
}

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
	viper.SetDefault("api.http_port", 7999)
	viper.SetDefault("api.grpc_port", 7998)
	viper.SetDefault("sync.enabled", true)
	viper.SetDefault("sync.strategy", "realtime")
	viper.SetDefault("sync.interval_minutes", 15)

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
