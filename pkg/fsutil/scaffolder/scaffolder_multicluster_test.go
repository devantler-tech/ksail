package scaffolder_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/scaffolder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMultiClusterScaffolder builds a scaffolder for a Kind cluster with the
// conventional k8s source directory and the given multi-cluster environment.
func newMultiClusterScaffolder(env string, writer io.Writer) *scaffolder.Scaffolder {
	cluster := v1alpha1.NewCluster()
	cluster.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla
	cluster.Spec.Workload.SourceDirectory = "k8s"

	return scaffolder.NewScaffolder(*cluster, writer, nil).
		WithDevcontainer(false).
		WithMultiClusterEnv(env)
}

func TestScaffoldMultiClusterWritesLayoutAndPointsSyncPathAtOverlay(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	scaffolderInstance := newMultiClusterScaffolder("local", io.Discard)

	err := scaffolderInstance.Scaffold(tempDir, false)
	require.NoError(t, err)

	baseContent, err := os.ReadFile(
		filepath.Join(tempDir, "k8s", "clusters", "base", "kustomization.yaml"),
	)
	require.NoError(t, err)
	assert.NotContains(t, string(baseContent), "../base")

	overlayContent, err := os.ReadFile(
		filepath.Join(tempDir, "k8s", "clusters", "local", "kustomization.yaml"),
	)
	require.NoError(t, err)
	assert.Contains(t, string(overlayContent), "../base")

	ksailContent, err := os.ReadFile(filepath.Join(tempDir, "ksail.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(ksailContent), "kustomizationFile: clusters/local")

	// The environment overlay replaces the flat single-cluster kustomization.
	_, err = os.Stat(filepath.Join(tempDir, "k8s", "kustomization.yaml"))
	assert.True(t, os.IsNotExist(err))
}

func TestScaffoldMultiClusterRespectsExplicitKustomizationFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cluster := v1alpha1.NewCluster()
	cluster.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla
	cluster.Spec.Workload.SourceDirectory = "k8s"
	cluster.Spec.Workload.KustomizationFile = "custom/entry"

	scaffolderInstance := scaffolder.NewScaffolder(*cluster, io.Discard, nil).
		WithDevcontainer(false).
		WithMultiClusterEnv("local")

	err := scaffolderInstance.Scaffold(tempDir, false)
	require.NoError(t, err)

	ksailContent, err := os.ReadFile(filepath.Join(tempDir, "ksail.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(ksailContent), "kustomizationFile: custom/entry")
	assert.NotContains(t, string(ksailContent), "clusters/local")

	// The layout is still seeded so the user can repoint at it later.
	_, err = os.Stat(filepath.Join(tempDir, "k8s", "clusters", "local", "kustomization.yaml"))
	require.NoError(t, err)
}

func TestScaffoldMultiClusterInvalidEnvFailsBeforeWriting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  string
	}{
		{name: "reserved base name", env: "base"},
		{name: "invalid DNS-1123 name", env: "Not_Valid"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			scaffolderInstance := newMultiClusterScaffolder(testCase.env, io.Discard)

			err := scaffolderInstance.Scaffold(tempDir, false)
			require.Error(t, err)
			require.ErrorContains(t, err, "failed to derive multi-cluster layout")

			// Fail-fast: nothing was scaffolded, not even ksail.yaml.
			_, statErr := os.Stat(filepath.Join(tempDir, "ksail.yaml"))
			assert.True(t, os.IsNotExist(statErr))
		})
	}
}

func TestScaffoldMultiClusterIsIdempotentWithoutForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	scaffolderInstance := newMultiClusterScaffolder("local", io.Discard)

	err := scaffolderInstance.Scaffold(tempDir, false)
	require.NoError(t, err)

	overlayPath := filepath.Join(tempDir, "k8s", "clusters", "local", "kustomization.yaml")
	before, err := os.ReadFile(overlayPath)
	require.NoError(t, err)

	buffer := &bytes.Buffer{}
	rerunInstance := newMultiClusterScaffolder("local", buffer)

	err = rerunInstance.Scaffold(tempDir, false)
	require.NoError(t, err)

	after, err := os.ReadFile(overlayPath)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
	assert.Contains(t, buffer.String(), "skipped")
	assert.Contains(
		t,
		buffer.String(),
		filepath.Join("k8s", "clusters", "local", "kustomization.yaml"),
	)
}
