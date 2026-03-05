package oci_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/client/oci"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test sentinel errors for push retry behavior tests.
var (
	errRedirectLimit = errors.New(
		`get "https://ghcr.io/v2/token": stopped after 10 redirects`,
	)
	errPushBadGateway   = errors.New("502 Bad Gateway")
	errPushNonRetryable = errors.New("invalid reference format")
	errPushIOTimeout    = errors.New("dial tcp 1.2.3.4:443: i/o timeout")
	errPushConnReset    = errors.New("connection reset by peer")
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

// mockPushFn creates a mock push function that returns errors from the given
// list per attempt, tracking call count via the atomic counter.
func mockPushFn(
	callCount *atomic.Int32,
	errs []error,
) oci.PushFn {
	return func(_ name.Reference, _ v1.Image, _ ...remote.Option) error {
		if len(errs) == 0 {
			return nil
		}

		idx := int(callCount.Add(1)) - 1
		if idx < len(errs) {
			return errs[idx]
		}

		return errs[len(errs)-1]
	}
}

func TestPushWithRetry_SucceedsOnFirstAttempt(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	push := mockPushFn(&callCount, []error{nil})

	ref, err := name.ParseReference("localhost:5000/test:v1", name.Insecure)
	require.NoError(t, err)

	err = oci.PushWithRetry(
		context.Background(), ref, nil, nil, push,
	)

	require.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())
}

func TestPushWithRetry_RetriesRedirectLimitError(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	push := mockPushFn(&callCount, []error{
		errRedirectLimit, errRedirectLimit, nil,
	})

	ref, err := name.ParseReference("localhost:5000/test:v1", name.Insecure)
	require.NoError(t, err)

	err = oci.PushWithRetry(
		context.Background(), ref, nil, nil, push,
	)

	require.NoError(t, err)
	assert.Equal(t, int32(3), callCount.Load())
}

func TestPushWithRetry_RetriesTransientErrors(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	push := mockPushFn(&callCount, []error{
		errPushBadGateway, nil,
	})

	ref, err := name.ParseReference("localhost:5000/test:v1", name.Insecure)
	require.NoError(t, err)

	err = oci.PushWithRetry(
		context.Background(), ref, nil, nil, push,
	)

	require.NoError(t, err)
	assert.Equal(t, int32(2), callCount.Load())
}

func TestPushWithRetry_NonRetryableStopsImmediately(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	push := mockPushFn(&callCount, []error{errPushNonRetryable})

	ref, err := name.ParseReference("localhost:5000/test:v1", name.Insecure)
	require.NoError(t, err)

	err = oci.PushWithRetry(
		context.Background(), ref, nil, nil, push,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "push failed (non-retryable)")
	assert.Contains(t, err.Error(), "invalid reference format")
	assert.Equal(t, int32(1), callCount.Load())
}

func TestPushWithRetry_AllAttemptsExhausted(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	push := mockPushFn(&callCount, []error{errPushIOTimeout})

	ref, err := name.ParseReference("localhost:5000/test:v1", name.Insecure)
	require.NoError(t, err)

	err = oci.PushWithRetry(
		context.Background(), ref, nil, nil, push,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "push failed after 3 attempts")
	assert.Contains(t, err.Error(), "i/o timeout")
	assert.Equal(t, int32(3), callCount.Load())
}

func TestPushWithRetry_CancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var callCount atomic.Int32

	push := mockPushFn(&callCount, []error{errPushConnReset})

	ref, err := name.ParseReference("localhost:5000/test:v1", name.Insecure)
	require.NoError(t, err)

	err = oci.PushWithRetry(ctx, ref, nil, nil, push)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "push cancelled")
}
