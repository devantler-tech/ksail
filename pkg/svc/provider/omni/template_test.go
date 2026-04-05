package omni_test

import (
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildClusterTemplate_Basic(t *testing.T) {
	t.Parallel()

	reader, err := omni.BuildClusterTemplate(omni.TemplateParams{
		ClusterName:       "test-cluster",
		TalosVersion:      "1.11.2",
		KubernetesVersion: "1.32.0",
		ControlPlanes:     3,
		Workers:           2,
	})

	require.NoError(t, err)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	content := string(data)

	// Verify Cluster document
	assert.Contains(t, content, "kind: Cluster")
	assert.Contains(t, content, "name: test-cluster")
	assert.Contains(t, content, "version: v1.11.2")
	assert.Contains(t, content, "version: v1.32.0")

	// Verify ControlPlane document
	assert.Contains(t, content, "kind: ControlPlane")
	assert.Contains(t, content, "size: 3")

	// Verify Workers document
	assert.Contains(t, content, "kind: Workers")
	assert.Contains(t, content, "size: 2")
}

func TestBuildClusterTemplate_NoWorkers(t *testing.T) {
	t.Parallel()

	reader, err := omni.BuildClusterTemplate(omni.TemplateParams{
		ClusterName:       "single-node",
		TalosVersion:      "v1.11.2",
		KubernetesVersion: "v1.32.0",
		ControlPlanes:     1,
		Workers:           0,
	})

	require.NoError(t, err)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	content := string(data)

	// Verify no Workers document
	assert.Contains(t, content, "kind: ControlPlane")
	assert.NotContains(t, content, "kind: Workers")
}

func TestBuildClusterTemplate_VersionPrefix(t *testing.T) {
	t.Parallel()

	reader, err := omni.BuildClusterTemplate(omni.TemplateParams{
		ClusterName:       "test",
		TalosVersion:      "v1.11.2", // Already has v prefix
		KubernetesVersion: "1.32.0",  // No v prefix
		ControlPlanes:     1,
	})

	require.NoError(t, err)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	content := string(data)

	// Both should have v prefix
	assert.Contains(t, content, "version: v1.11.2")
	assert.Contains(t, content, "version: v1.32.0")
}

func TestBuildClusterTemplate_EmptyTalosVersion(t *testing.T) {
	t.Parallel()

	_, err := omni.BuildClusterTemplate(omni.TemplateParams{
		ClusterName:       "test",
		TalosVersion:      "",
		KubernetesVersion: "1.32.0",
		ControlPlanes:     1,
	})

	require.ErrorIs(t, err, omni.ErrTalosVersionRequired)
}

func TestBuildClusterTemplate_EmptyKubernetesVersion(t *testing.T) {
	t.Parallel()

	_, err := omni.BuildClusterTemplate(omni.TemplateParams{
		ClusterName:       "test",
		TalosVersion:      "1.11.2",
		KubernetesVersion: "",
		ControlPlanes:     1,
	})

	require.ErrorIs(t, err, omni.ErrKubernetesVersionRequired)
}

func TestBuildClusterTemplate_WithPatches(t *testing.T) {
	t.Parallel()

	patches := []omni.PatchInfo{
		{
			Path:    "cluster/allow-scheduling.yaml",
			Scope:   omni.PatchScopeCluster,
			Content: []byte("cluster:\n  allowSchedulingOnControlPlanes: true\n"),
		},
		{
			Path:    "control-planes/network.yaml",
			Scope:   omni.PatchScopeControlPlane,
			Content: []byte("machine:\n  network:\n    hostname: cp\n"),
		},
		{
			Path:    "workers/labels.yaml",
			Scope:   omni.PatchScopeWorker,
			Content: []byte("machine:\n  nodeLabels:\n    role: worker\n"),
		},
	}

	reader, err := omni.BuildClusterTemplate(omni.TemplateParams{
		ClusterName:       "patched-cluster",
		TalosVersion:      "v1.11.2",
		KubernetesVersion: "v1.32.0",
		ControlPlanes:     1,
		Workers:           1,
		Patches:           patches,
	})

	require.NoError(t, err)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	content := string(data)

	// Verify patches are included
	assert.Contains(t, content, "name: allow-scheduling")
	assert.Contains(t, content, "allowSchedulingOnControlPlanes: true")
	assert.Contains(t, content, "name: network")
	assert.Contains(t, content, "hostname: cp")
	assert.Contains(t, content, "name: labels")
	assert.Contains(t, content, "role: worker")
}

func TestBuildClusterTemplate_PatchWithEmptyLines(t *testing.T) {
	t.Parallel()

	// Patch content with an empty line — exercises the empty-line branch in writeInlineContent.
	patches := []omni.PatchInfo{
		{
			Path:  "cluster/multi-line.yaml",
			Scope: omni.PatchScopeCluster,
			Content: []byte("machine:\n  network:\n\n    hostname: test\n"),
		},
	}

	reader, err := omni.BuildClusterTemplate(omni.TemplateParams{
		ClusterName:       "test-cluster",
		TalosVersion:      "v1.11.2",
		KubernetesVersion: "v1.32.0",
		ControlPlanes:     1,
		Patches:           patches,
	})

	require.NoError(t, err)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "hostname: test")
	assert.Contains(t, content, "name: multi-line")
}
