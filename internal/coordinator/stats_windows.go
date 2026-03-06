package coordinator

import (
	"golang.org/x/sys/windows"
)

// getDiskSpace returns used and total disk space in bytes for the given path
func getDiskSpace(path string) (uint64, uint64) {
	var freeBytesAvailable uint64
	var totalNumberOfBytes uint64
	var totalNumberOfFreeBytes uint64

	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0
	}

	err = windows.GetDiskFreeSpaceEx(pathPtr, &freeBytesAvailable, &totalNumberOfBytes, &totalNumberOfFreeBytes)
	if err != nil {
		return 0, 0
	}

	return totalNumberOfBytes - totalNumberOfFreeBytes, totalNumberOfBytes
}
