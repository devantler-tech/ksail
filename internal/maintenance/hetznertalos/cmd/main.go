// Package main exposes the Hetzner Talos ISO catalog updater to automation.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v7/internal/maintenance/hetznertalos"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

func main() {
	root := flag.String("root", ".", "KSail repository root")
	feedFile := flag.String("feed-file", "", "local RSS fixture instead of the official feed")

	flag.Parse()

	rootPath, feedPath, err := canonicalizePaths(*root, *feedFile)
	if err == nil {
		err = hetznertalos.Run(context.Background(), rootPath, feedPath, os.Stdout)
	}
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "update Hetzner Talos ISO: %v\n", err)

		os.Exit(1)
	}
}

func canonicalizePaths(root, feedFile string) (string, string, error) {
	canonicalRoot, err := fsutil.EvalCanonicalPath(root)
	if err != nil {
		return "", "", fmt.Errorf("canonicalize repository root: %w", err)
	}

	if strings.TrimSpace(feedFile) == "" {
		return canonicalRoot, "", nil
	}

	canonicalFeed, err := fsutil.EvalCanonicalPath(feedFile)
	if err != nil {
		return "", "", fmt.Errorf("canonicalize changelog fixture: %w", err)
	}

	return canonicalRoot, canonicalFeed, nil
}
