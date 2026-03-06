package coordinator

import (
	"syscall"
)

// getDiskSpace returns used and total disk space in bytes for the given path
func getDiskSpace(path string) (uint64, uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0
	}
	// Blocks * Size to get total bytes
	total := stat.Blocks * uint64(stat.Bsize)
	// (Blocks - Bfree) * Size to get used bytes
	used := (stat.Blocks - stat.Bfree) * uint64(stat.Bsize)
	return used, total
}
