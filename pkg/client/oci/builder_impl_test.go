package oci_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail-go/pkg/client/oci"
	"github.com/stretchr/testify/require"
)

func TestNewWorkloadArtifactBuilder(t *testing.T) {
	t.Parallel()

	builder := oci.NewWorkloadArtifactBuilder()

	require.NotNil(t, builder)
}

// buildWithTempDir is a test helper that creates a builder, temp directory, and calls Build.
func buildWithTempDir(t *testing.T, sourceDir string) error {
	t.Helper()

	builder := oci.NewWorkloadArtifactBuilder()

	_, err := builder.Build(context.Background(), oci.BuildOptions{
		SourcePath:       sourceDir,
		RegistryEndpoint: "localhost:5000",
		Version:          "1.0.0",
	})
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	return nil
}

func TestBuild(t *testing.T) {
	t.Parallel()

	t.Run("fails with invalid options", func(t *testing.T) {
		t.Parallel()

		builder := oci.NewWorkloadArtifactBuilder()

		_, err := builder.Build(context.Background(), oci.BuildOptions{})

		require.ErrorIs(t, err, oci.ErrSourcePathRequired)
	})

	t.Run("fails when source directory is empty", func(t *testing.T) {
		t.Parallel()

		sourceDir := t.TempDir()

		err := buildWithTempDir(t, sourceDir)

		require.ErrorIs(t, err, oci.ErrNoManifestFiles)
	})

	t.Run("fails when source contains only non-manifest files", func(t *testing.T) {
		t.Parallel()

		sourceDir := t.TempDir()

		// Create non-manifest files
		require.NoError(
			t,
			os.WriteFile(filepath.Join(sourceDir, "README.md"), []byte("# Test"), 0o600),
		)
		require.NoError(
			t,
			os.WriteFile(filepath.Join(sourceDir, "script.sh"), []byte("#!/bin/bash"), 0o600),
		)

		err := buildWithTempDir(t, sourceDir)

		require.ErrorIs(t, err, oci.ErrNoManifestFiles)
	})

	t.Run("fails when manifest file is empty", func(t *testing.T) {
		t.Parallel()

		sourceDir := t.TempDir()

		// Create empty manifest file
		emptyFile := filepath.Join(sourceDir, "empty.yaml")
		require.NoError(t, os.WriteFile(emptyFile, []byte(""), 0o600))

		err := buildWithTempDir(t, sourceDir)

		require.Error(t, err)
		require.Contains(t, err.Error(), "empty")
	})

	// Note: We cannot test successful builds without a running registry.
	// Integration tests should cover the full push workflow.
}
