package registry_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	result, err := registry.PushOCIArtifact(context.Background(), registry.PushOCIArtifactOptions{
		ClusterConfig: clusterCfg,
		ClusterName:   "test-cluster",
	})

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
	_, err := registry.PushOCIArtifact(context.Background(), registry.PushOCIArtifactOptions{
		ClusterConfig: clusterCfg,
		ClusterName:   "test-cluster",
	})
	// Will error due to registry resolution, but the directory should be found
	// (error should be about pushing/registry, not about source directory)
	if err != nil {
		assert.NotContains(t, err.Error(), "source directory not found")
	}
}
