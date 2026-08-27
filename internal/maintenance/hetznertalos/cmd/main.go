// Package main exposes the Hetzner Talos ISO catalog updater to automation.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/internal/maintenance/hetznertalos"
)

func main() {
	root := flag.String("root", ".", "KSail repository root")
	feedFile := flag.String("feed-file", "", "local RSS fixture instead of the official feed")

	flag.Parse()

	err := hetznertalos.Run(context.Background(), *root, *feedFile, os.Stdout)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "update Hetzner Talos ISO: %v\n", err)

		os.Exit(1)
	}
}
