package helpers_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/io"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPushOCIArtifact_SkipIfMissing(t *testing.T) {
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

	// Should skip gracefully when directory doesn't exist and SkipIfMissing is true
	result, err := helpers.PushOCIArtifact(context.Background(), helpers.PushOCIArtifactOptions{
		ClusterConfig: clusterCfg,
		ClusterName:   "test-cluster",
		SkipIfMissing: true,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Pushed, "expected Pushed to be false when directory is missing")
}

func TestPushOCIArtifact_ErrorIfMissing(t *testing.T) {
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

	// Should return error when directory doesn't exist and SkipIfMissing is false
	result, err := helpers.PushOCIArtifact(context.Background(), helpers.PushOCIArtifactOptions{
		ClusterConfig: clusterCfg,
		ClusterName:   "test-cluster",
		SkipIfMissing: false,
	})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, io.ErrSourceDirectoryNotFound)
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
	_, err := helpers.PushOCIArtifact(context.Background(), helpers.PushOCIArtifactOptions{
		ClusterConfig: clusterCfg,
		ClusterName:   "test-cluster",
		SkipIfMissing: false,
	})
	// Will error due to registry resolution, but not due to missing directory
	if err != nil {
		assert.NotErrorIs(t, err, io.ErrSourceDirectoryNotFound)
	}
}
