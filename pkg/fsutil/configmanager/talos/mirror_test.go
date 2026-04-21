package talos_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfig "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ApplyMirrorRegistries — additional cases
// ---------------------------------------------------------------------------

// TestConfigs_ApplyMirrorRegistries_NilBundle verifies that ApplyMirrorRegistries
// is a no-op when the bundle is nil.
func TestConfigs_ApplyMirrorRegistries_NilBundle(t *testing.T) {
	t.Parallel()

	configs := &talosconfig.Configs{}
	err := configs.ApplyMirrorRegistries([]talosconfig.MirrorRegistry{
		{Host: "docker.io", Endpoints: []string{"http://mirror:5000"}},
	})
	require.NoError(t, err)
}

// TestConfigs_ApplyMirrorRegistries_WithMirror verifies that mirror registries
// are applied to both control plane and worker configs.
func TestConfigs_ApplyMirrorRegistries_WithMirror(t *testing.T) {
	t.Parallel()

	configs, err := talosconfig.NewDefaultConfigs()
	require.NoError(t, err)

	mirrors := []talosconfig.MirrorRegistry{
		{
			Host:      "docker.io",
			Endpoints: []string{"http://mirror.local:5000"},
		},
	}

	err = configs.ApplyMirrorRegistries(mirrors)
	require.NoError(t, err)

	// Verify mirrors were applied
	hosts := configs.ExtractMirrorHosts()
	assert.Contains(t, hosts, "docker.io")
}

// TestConfigs_ApplyMirrorRegistries_WithAuth verifies that mirror registries
// with authentication credentials are applied.
func TestConfigs_ApplyMirrorRegistries_WithAuth(t *testing.T) {
	t.Parallel()

	configs, err := talosconfig.NewDefaultConfigs()
	require.NoError(t, err)

	mirrors := []talosconfig.MirrorRegistry{
		{
			Host:      "ghcr.io",
			Endpoints: []string{"http://ghcr.io:5000"},
			Username:  "testuser",
			Password:  "testpass",
		},
	}

	err = configs.ApplyMirrorRegistries(mirrors)
	require.NoError(t, err)

	// Verify mirrors were applied
	hosts := configs.ExtractMirrorHosts()
	assert.Contains(t, hosts, "ghcr.io")
}

// ---------------------------------------------------------------------------
// WithEndpoint — additional cases
// ---------------------------------------------------------------------------

// TestConfigs_WithEndpoint_DifferentEndpoint verifies that WithEndpoint with a
// different endpoint regenerates configs preserving the cluster name.
func TestConfigs_WithEndpoint_DifferentEndpoint(t *testing.T) {
	t.Parallel()

	original, err := talosconfig.NewDefaultConfigs()
	require.NoError(t, err)

	updated, err := original.WithEndpoint("10.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, updated)

	// Cluster name should be preserved
	assert.Equal(t, original.GetClusterName(), updated.GetClusterName())
}

// TestConfigs_WithEndpoint_SameEndpoint verifies that WithEndpoint with the
// same endpoint returns the same instance.
func TestConfigs_WithEndpoint_SameEndpoint(t *testing.T) {
	t.Parallel()

	original, err := talosconfig.NewDefaultConfigs()
	require.NoError(t, err)

	// First set a new endpoint
	updated, err := original.WithEndpoint("10.0.0.1")
	require.NoError(t, err)

	// Then call with the same endpoint again
	same, err := updated.WithEndpoint("10.0.0.1")
	require.NoError(t, err)
	assert.Same(t, updated, same)
}

// ---------------------------------------------------------------------------
// WithName — additional cases
// ---------------------------------------------------------------------------

// TestConfigs_WithName_CustomName verifies WithName with a different name
// regenerates the bundle.
func TestConfigs_WithName_CustomName(t *testing.T) {
	t.Parallel()

	original, err := talosconfig.NewDefaultConfigs()
	require.NoError(t, err)

	updated, err := original.WithName("custom-cluster")
	require.NoError(t, err)
	assert.Equal(t, "custom-cluster", updated.GetClusterName())

	// Original unchanged
	assert.Equal(t, talosconfig.DefaultClusterName, original.GetClusterName())
}

// ---------------------------------------------------------------------------
// ResolveClusterName — additional cases
// ---------------------------------------------------------------------------

// TestResolveClusterName_TalosContext verifies Talos cluster name resolution.
//
//nolint:funlen // Table-driven test coverage is naturally long.
func TestResolveClusterName_TalosContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cluster  *v1alpha1.Cluster
		talos    *talosconfig.Configs
		expected string
	}{
		{
			name:     "nil cluster and nil talos returns default",
			cluster:  nil,
			talos:    nil,
			expected: talosconfig.DefaultClusterName,
		},
		{
			name: "admin@ prefix extracts name",
			cluster: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Connection: v1alpha1.Connection{
							Context: "admin@my-cluster",
						},
					},
				},
			},
			talos:    nil,
			expected: "my-cluster",
		},
		{
			name: "no recognized prefix returns context as-is",
			cluster: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Connection: v1alpha1.Connection{
							Context: "default",
						},
					},
				},
			},
			talos:    nil,
			expected: "default",
		},
		{
			name: "empty context returns default",
			cluster: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Connection: v1alpha1.Connection{
							Context: "",
						},
					},
				},
			},
			talos:    nil,
			expected: talosconfig.DefaultClusterName,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := talosconfig.ResolveClusterName(tc.cluster, tc.talos)
			assert.Equal(t, tc.expected, result)
		})
	}
}
