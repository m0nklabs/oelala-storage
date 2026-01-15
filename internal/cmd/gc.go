// Package cmd provides the command-line interface for oelala-storage.
package cmd

import (
	"fmt"

	"github.com/m0nklabs/oelala-storage/internal/config"
	"github.com/m0nklabs/oelala-storage/internal/gc"
	"github.com/m0nklabs/oelala-storage/internal/metadata"
	"github.com/m0nklabs/oelala-storage/internal/storage"
	"github.com/spf13/cobra"
)

var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Run garbage collection on expired files",
	Long: `Run garbage collection to delete expired files.

Files are marked for expiration via the X-Expires-At header when uploaded.
This is typically set by the backend (oelala) based on user tier.

Examples:
  # Dry run - show what would be deleted
  oelala-storage gc --dry-run

  # Actually delete expired files
  oelala-storage gc

  # Show stats only
  oelala-storage gc --stats`,
	RunE: runGC,
}

var (
	gcDryRun    bool
	gcStatsOnly bool
)

func init() {
	rootCmd.AddCommand(gcCmd)
	gcCmd.Flags().BoolVar(&gcDryRun, "dry-run", false, "Show what would be deleted without actually deleting")
	gcCmd.Flags().BoolVar(&gcStatsOnly, "stats", false, "Show GC stats only (no deletion)")
}

func runGC(cmd *cobra.Command, _ []string) error {
	// Load config
	configPath := "oelala-storage.yaml"
	if cfgFile != "" {
		configPath = cfgFile
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize storage
	store, err := storage.NewStore(cfg.Storage.Path, cfg.Storage.MaxSizeGB)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Initialize metadata store
	metadataPath := cfg.Storage.Path + "/metadata"
	metadataStore, err := metadata.NewStore(metadata.DefaultOptions(metadataPath))
	if err != nil {
		return fmt.Errorf("failed to initialize metadata store: %w", err)
	}
	defer metadataStore.Close()

	if gcStatsOnly {
		// Just list expired files without deleting
		expired, err := metadataStore.ListExpired()
		if err != nil {
			return fmt.Errorf("failed to list expired files: %w", err)
		}

		if len(expired) == 0 {
			fmt.Println("✅ No expired files found")
			return nil
		}

		var totalSize int64
		fmt.Printf("📋 Found %d expired files:\n\n", len(expired))
		for _, meta := range expired {
			fmt.Printf("  • %s/%s (%d bytes, expired: %s)\n",
				meta.Bucket, meta.Key, meta.Size, meta.ExpiresAt.Format("2006-01-02 15:04:05"))
			totalSize += meta.Size
		}
		fmt.Printf("\nTotal: %d files, %s\n", len(expired), humanBytes(totalSize))
		return nil
	}

	// Create collector and run once
	collector := gc.NewCollector(store, metadataStore, gc.Config{
		DryRun: gcDryRun,
	})

	stats := collector.RunOnce()

	if gcDryRun {
		fmt.Printf("🗑️ DRY RUN: Would delete %d files (%s)\n",
			stats.FilesDeleted, humanBytes(stats.BytesFreed))
	} else {
		fmt.Printf("🗑️ Deleted %d files (%s) in %dms\n",
			stats.FilesDeleted, humanBytes(stats.BytesFreed), stats.RunDurationMs)
	}

	if stats.Errors > 0 {
		fmt.Printf("⚠️ Errors: %d\n", stats.Errors)
	}

	return nil
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
