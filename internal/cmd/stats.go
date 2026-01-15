// Package cmd provides the command-line interface for oelala-storage.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dustin/go-humanize"
	"github.com/m0nklabs/oelala-storage/internal/config"
	"github.com/m0nklabs/oelala-storage/internal/dedup"
	"github.com/m0nklabs/oelala-storage/internal/metadata"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show storage node statistics",
	Long: `Display statistics about the storage node including:
  - Disk usage and available space
  - Total files stored
  - Deduplication savings
  - Metadata store info

Examples:
  oelala-storage stats
  oelala-storage stats --json`,
	RunE: runStats,
}

var statsJSON bool

func init() {
	rootCmd.AddCommand(statsCmd)
	statsCmd.Flags().BoolVar(&statsJSON, "json", false, "Output in JSON format")
}

func runStats(cmd *cobra.Command, _ []string) error {
	// Load config
	configPath := "oelala-storage.yaml"
	if cfgFile != "" {
		configPath = cfgFile
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	storagePath := cfg.Storage.Path

	// Calculate disk usage
	var totalSize int64
	var fileCount int64
	err = filepath.Walk(storagePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if !info.IsDir() {
			totalSize += info.Size()
			fileCount++
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk storage path: %w", err)
	}

	// Get dedup stats if available
	dedupPath := filepath.Join(storagePath, "dedup")
	var dedupStats *dedup.Stats
	if _, err := os.Stat(dedupPath); err == nil {
		dedupStore, err := dedup.NewStore(dedup.Options{
			BlobPath: filepath.Join(dedupPath, "blobs"),
			DBPath:   filepath.Join(dedupPath, "db"),
		})
		if err == nil {
			stats, _ := dedupStore.GetStats()
			dedupStats = stats
			dedupStore.Close()
		}
	}

	// Get metadata stats
	metadataPath := filepath.Join(storagePath, "metadata")
	var metaCount int64
	var expiredCount int64
	if _, err := os.Stat(metadataPath); err == nil {
		metaStore, err := metadata.NewStore(metadata.DefaultOptions(metadataPath))
		if err == nil {
			all, _ := metaStore.ListByBucket("")
			metaCount = int64(len(all))
			expired, _ := metaStore.ListExpired()
			expiredCount = int64(len(expired))
			metaStore.Close()
		}
	}

	if statsJSON {
		// JSON output
		fmt.Printf(`{
  "storage_path": %q,
  "total_bytes": %d,
  "total_files": %d,
  "metadata_count": %d,
  "expired_count": %d`, storagePath, totalSize, fileCount, metaCount, expiredCount)
		if dedupStats != nil {
			fmt.Printf(`,
  "dedup": {
    "total_blobs": %d,
    "total_refs": %d,
    "bytes_stored": %d,
    "logical_bytes": %d,
    "bytes_saved": %d,
    "savings_pct": %.2f
  }`, dedupStats.TotalBlobs, dedupStats.TotalReferences,
				dedupStats.TotalBytes, dedupStats.LogicalBytes,
				dedupStats.BytesSaved, dedupStats.DeduplicationPct)
		}
		fmt.Println("\n}")
		return nil
	}

	// Human-readable output
	fmt.Println("📊 oelala-storage Statistics")
	fmt.Println("════════════════════════════════════════")
	fmt.Printf("📁 Storage Path:    %s\n", storagePath)
	fmt.Printf("💾 Total Size:      %s\n", humanize.Bytes(uint64(totalSize)))
	fmt.Printf("📄 Total Files:     %s\n", humanize.Comma(fileCount))
	fmt.Printf("🗂️  Metadata Count:  %s\n", humanize.Comma(metaCount))
	if expiredCount > 0 {
		fmt.Printf("⏰ Expired Files:   %s (run 'oelala-storage gc' to clean)\n", humanize.Comma(expiredCount))
	}

	if dedupStats != nil && dedupStats.TotalBlobs > 0 {
		fmt.Println()
		fmt.Println("🔗 Deduplication")
		fmt.Println("────────────────────────────────────────")
		fmt.Printf("   Unique Blobs:    %s\n", humanize.Comma(int64(dedupStats.TotalBlobs)))
		fmt.Printf("   References:      %s\n", humanize.Comma(int64(dedupStats.TotalReferences)))
		fmt.Printf("   Actual Storage:  %s\n", humanize.Bytes(uint64(dedupStats.TotalBytes)))
		fmt.Printf("   Logical Size:    %s\n", humanize.Bytes(uint64(dedupStats.LogicalBytes)))
		fmt.Printf("   Space Saved:     %s (%.1f%%)\n",
			humanize.Bytes(uint64(dedupStats.BytesSaved)),
			dedupStats.DeduplicationPct)
	}

	fmt.Println("════════════════════════════════════════")
	return nil
}
