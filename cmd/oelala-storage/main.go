package main

import (
	"fmt"
	"os"

	"github.com/m0nklabs/oelala-storage/internal/cmd"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	if err := cmd.Execute(Version, BuildTime); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
