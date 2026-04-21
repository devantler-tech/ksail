package registryresolver_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/registryresolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRetryBaseWait and testRetryMaxWait are minimal delays so retry
// tests complete in milliseconds instead of minutes.
const (
	testRetryBaseWait = 1 * time.Millisecond
	testRetryMaxWait  = 5 * time.Millisecond
)

func TestPushOCIArtifact_MissingDirectory_PushesEmptyArtifact(t *testing.T) {
	t.Parallel()

	// Create a test cluster config
	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "localhost:5000",
				},
			},
			Workload: v1alpha1.WorkloadSpec{
				SourceDirectory: "/nonexistent/directory",
			},
		},
	}

	// Should attempt to push empty artifact when directory doesn't exist
	// Note: This will fail at the registry push stage since we don't have a real registry,
	// but it should NOT return early due to missing directory
	result, err := registryresolver.PushOCIArtifact(
		context.Background(),
		registryresolver.PushOCIArtifactOptions{
			ClusterConfig: clusterCfg,
			ClusterName:   "test-cluster",
		},
	)

	// Will error due to registry connection, but should attempt the push
	// The error should be about pushing, not about missing directory
	if err != nil {
		assert.Contains(
			t,
			err.Error(),
			"push",
			"expected error to be about pushing, not missing directory",
		)
	} else {
		require.NotNil(t, result)
		assert.True(t, result.Pushed, "expected Pushed to be true")
		assert.True(t, result.Empty, "expected Empty to be true when directory is missing")
	}
}

func TestPushOCIArtifact_IncompleteExternalCredentials(t *testing.T) {
	t.Parallel()

	// External registry with username but no password — simulates
	// GITHUB_ACTOR being set but GITHUB_TOKEN missing.
	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "myuser@ghcr.io/org/repo",
				},
			},
			Workload: v1alpha1.WorkloadSpec{
				SourceDirectory: "/nonexistent/directory",
			},
		},
	}

	_, err := registryresolver.PushOCIArtifact(
		context.Background(),
		registryresolver.PushOCIArtifactOptions{
			ClusterConfig: clusterCfg,
			ClusterName:   "test-cluster",
		},
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, registryresolver.ErrExternalRegistryCredentialsIncomplete)
}

//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir()
func TestPushOCIArtifact_UsesDefaultSourceDir(t *testing.T) {
	// Note: Cannot use t.Parallel() when using t.Chdir()

	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create the default source directory (k8s)
	k8sDir := filepath.Join(tmpDir, "k8s")
	require.NoError(t, os.MkdirAll(k8sDir, 0o750))

	// Create a test file
	testFile := filepath.Join(k8sDir, "test.yaml")
	require.NoError(t, os.WriteFile(testFile, []byte("test: data"), 0o600))

	// Change to the temp directory using t.Chdir
	t.Chdir(tmpDir)

	// Create a test cluster config with no source directory specified
	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "localhost:5000",
				},
			},
			Workload: v1alpha1.WorkloadSpec{
				// Empty - should use default "k8s"
			},
		},
	}

	// This will fail to resolve registry, but we're just testing directory resolution
	_, err := registryresolver.PushOCIArtifact(
		context.Background(),
		registryresolver.PushOCIArtifactOptions{
			ClusterConfig: clusterCfg,
			ClusterName:   "test-cluster",
		},
	)
	// Will error due to registry resolution, but the directory should be found
	// (error should be about pushing/registry, not about source directory)
	if err != nil {
		assert.NotContains(t, err.Error(), "source directory not found")
	}
}

// Test sentinel errors for retry behavior tests.
var (
	errGHCRBadGateway   = errors.New("502 Bad Gateway")
	errGHCRNonRetryable = errors.New("denied: permission_denied: write_package")
	errGHCRIOTimeout    = errors.New("dial tcp 1.2.3.4:443: i/o timeout")
	errGHCRConnReset    = errors.New("connection reset by peer")
)

// mockPushFn creates a mock push function that returns errors from the given
// list per attempt, tracking call count via the atomic counter.
// When all errors are consumed, it returns the last error in the list.
func mockPushFn(
	callCount *atomic.Int32,
	errs []error,
) func() (*registryresolver.PushOCIArtifactResult, error) {
	return func() (*registryresolver.PushOCIArtifactResult, error) {
		if len(errs) == 0 {
			return &registryresolver.PushOCIArtifactResult{Pushed: true, Empty: false}, nil
		}

		idx := int(callCount.Add(1)) - 1

		var err error
		if idx < len(errs) {
			err = errs[idx]
		} else {
			err = errs[len(errs)-1]
		}

		if err != nil {
			return nil, err
		}

		return &registryresolver.PushOCIArtifactResult{Pushed: true, Empty: false}, nil
	}
}

//nolint:paralleltest // Mutates shared package-level retry vars via SetExternalPushRetryParams
func TestRetryExternalPush_SucceedsOnFirstAttempt(t *testing.T) {
	t.Cleanup(registryresolver.SetExternalPushRetryParams(
		5, testRetryBaseWait, testRetryMaxWait,
	))

	var callCount atomic.Int32

	push := mockPushFn(&callCount, []error{nil})

	result, err := registryresolver.RetryExternalPush(context.Background(), push)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Pushed)
	assert.Equal(t, int32(1), callCount.Load())
}

//nolint:paralleltest // Mutates shared package-level retry vars via SetExternalPushRetryParams
func TestRetryExternalPush_RetriesTransientErrors(t *testing.T) {
	t.Cleanup(registryresolver.SetExternalPushRetryParams(
		5, testRetryBaseWait, testRetryMaxWait,
	))

	var callCount atomic.Int32

	push := mockPushFn(&callCount, []error{
		errGHCRBadGateway, nil,
	})

	result, err := registryresolver.RetryExternalPush(context.Background(), push)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Pushed)
	assert.Equal(t, int32(2), callCount.Load())
}

//nolint:paralleltest // Mutates shared package-level retry vars via SetExternalPushRetryParams
func TestRetryExternalPush_RetriesMultipleTransientErrors(t *testing.T) {
	t.Cleanup(registryresolver.SetExternalPushRetryParams(
		5, testRetryBaseWait, testRetryMaxWait,
	))

	var callCount atomic.Int32

	push := mockPushFn(&callCount, []error{
		errGHCRIOTimeout, errGHCRConnReset, errGHCRBadGateway, nil,
	})

	result, err := registryresolver.RetryExternalPush(context.Background(), push)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Pushed)
	assert.Equal(t, int32(4), callCount.Load())
}

//nolint:paralleltest // Mutates shared package-level retry vars via SetExternalPushRetryParams
func TestRetryExternalPush_NonRetryableStopsImmediately(t *testing.T) {
	t.Cleanup(registryresolver.SetExternalPushRetryParams(
		5, testRetryBaseWait, testRetryMaxWait,
	))

	var callCount atomic.Int32

	push := mockPushFn(&callCount, []error{errGHCRNonRetryable})

	result, err := registryresolver.RetryExternalPush(context.Background(), push)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "push to external registry failed (non-retryable)")
	assert.Contains(t, err.Error(), "permission_denied")
	assert.Equal(t, int32(1), callCount.Load())
}

//nolint:paralleltest // Mutates shared package-level retry vars via SetExternalPushRetryParams
func TestRetryExternalPush_AllAttemptsExhausted(t *testing.T) {
	t.Cleanup(registryresolver.SetExternalPushRetryParams(
		5, testRetryBaseWait, testRetryMaxWait,
	))

	var callCount atomic.Int32

	// All 5 attempts return a retryable error
	push := mockPushFn(&callCount, []error{errGHCRIOTimeout})

	result, err := registryresolver.RetryExternalPush(context.Background(), push)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "push to external registry failed after 5 attempts")
	assert.Contains(t, err.Error(), "i/o timeout")
	assert.Equal(t, int32(5), callCount.Load())
}

//nolint:paralleltest // Mutates shared package-level retry vars via SetExternalPushRetryParams
func TestRetryExternalPush_CancelledContext(t *testing.T) {
	t.Cleanup(registryresolver.SetExternalPushRetryParams(
		5, testRetryBaseWait, testRetryMaxWait,
	))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var callCount atomic.Int32

	push := mockPushFn(&callCount, []error{errGHCRConnReset})

	result, err := registryresolver.RetryExternalPush(ctx, push)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "push to external registry cancelled")
	assert.Equal(t, int32(1), callCount.Load())
}
