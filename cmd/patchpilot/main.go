package main

import (
	"fmt"
	"os"

	"github.com/TheOriUHD/patchpilot/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "patchpilot: %v\n", err)
		os.Exit(1)
	}
}
