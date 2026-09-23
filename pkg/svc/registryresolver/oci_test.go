package registryresolver_test

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v7/pkg/svc/registryresolver"
	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRetryBaseWait and testRetryMaxWait are minimal delays so retry
// tests complete in milliseconds instead of minutes.
const (
	testRetryBaseWait = 1 * time.Millisecond
	testRetryMaxWait  = 5 * time.Millisecond
)

// startTestRegistry serves an in-memory OCI registry on an ephemeral loopback
// port for the lifetime of the test and returns its host:port endpoint, so push
// tests never reach whatever happens to listen on a fixed local port.
func startTestRegistry(t *testing.T) string {
	t.Helper()

	server := httptest.NewServer(ggcrregistry.New(ggcrregistry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(server.Close)

	return strings.TrimPrefix(server.URL, "http://")
}

// registryCatalog lists the repositories the test registry holds.
func registryCatalog(ctx context.Context, t *testing.T, endpoint string) []string {
	t.Helper()

	reg, err := name.NewRegistry(endpoint, name.Insecure)
	require.NoError(t, err)

	repos, err := remote.Catalog(ctx, reg)
	require.NoError(t, err)

	return repos
}

// pulledArtifactFiles pulls endpoint/repository:tag from the test registry and
// returns the file contents of its single layer, keyed by archive path.
func pulledArtifactFiles(
	ctx context.Context,
	t *testing.T,
	endpoint, repository, tag string,
) map[string]string {
	t.Helper()

	ref, err := name.ParseReference(endpoint+"/"+repository+":"+tag, name.Insecure)
	require.NoError(t, err)

	img, err := remote.Image(ref, remote.WithContext(ctx))
	require.NoError(t, err)

	layers, err := img.Layers()
	require.NoError(t, err)
	require.Len(t, layers, 1, "a workload artifact carries exactly one layer")

	contents, err := layers[0].Uncompressed()
	require.NoError(t, err)

	defer func() { _ = contents.Close() }()

	files := map[string]string{}
	reader := tar.NewReader(contents)

	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}

		require.NoError(t, nextErr)

		data, readErr := io.ReadAll(reader)
		require.NoError(t, readErr)

		files[header.Name] = string(data)
	}

	return files
}

func TestPushOCIArtifact_MissingDirectory_PushesEmptyArtifact(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	endpoint := startTestRegistry(t)

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: endpoint + "/team/workloads",
				},
			},
			Workload: v1alpha1.WorkloadSpec{
				SourceDirectory: filepath.Join(t.TempDir(), "missing"),
			},
		},
	}

	result, err := registryresolver.PushOCIArtifact(
		ctx,
		registryresolver.PushOCIArtifactOptions{
			ClusterConfig: clusterCfg,
			ClusterName:   "test-cluster",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Pushed, "expected Pushed to be true")
	assert.True(t, result.Empty, "expected Empty to be true when directory is missing")

	assert.Equal(t, []string{"team/workloads"}, registryCatalog(ctx, t, endpoint),
		"the artifact must land in the requested repository and nowhere else")

	files := pulledArtifactFiles(
		ctx, t, endpoint, "team/workloads", registry.DefaultLocalArtifactTag,
	)
	require.Len(t, files, 1, "an empty artifact carries only an empty kustomization")
	assert.Contains(t, files["kustomization.yaml"], "kind: Kustomization")
	assert.Contains(t, files["kustomization.yaml"], "resources: []")
}

//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir()
func TestPushOCIArtifact_UsesDefaultSourceDir(t *testing.T) {
	ctx := t.Context()
	endpoint := startTestRegistry(t)

	// Create the default source directory (k8s) with one manifest and run from
	// its parent, so an unset source directory must resolve to it.
	tmpDir := t.TempDir()
	k8sDir := filepath.Join(tmpDir, v1alpha1.DefaultSourceDirectory)
	require.NoError(t, os.MkdirAll(k8sDir, 0o750))
	manifestPath := filepath.Join(k8sDir, "test.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte("test: data"), 0o600))

	t.Chdir(tmpDir)

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: endpoint,
				},
			},
			Workload: v1alpha1.WorkloadSpec{
				// Empty - should use the default "k8s" source directory
			},
		},
	}

	result, err := registryresolver.PushOCIArtifact(
		ctx,
		registryresolver.PushOCIArtifactOptions{
			ClusterConfig: clusterCfg,
			ClusterName:   "test-cluster",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Pushed, "expected Pushed to be true")
	assert.False(t, result.Empty, "expected a non-empty artifact from the default source directory")

	// With no repository configured, the repository is named after the source directory.
	assert.Equal(t, []string{v1alpha1.DefaultSourceDirectory}, registryCatalog(ctx, t, endpoint))

	files := pulledArtifactFiles(
		ctx, t, endpoint, v1alpha1.DefaultSourceDirectory, registry.DefaultLocalArtifactTag,
	)
	assert.Equal(t, map[string]string{"test.yaml": "test: data"}, files)
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
