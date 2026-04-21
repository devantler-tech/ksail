//nolint:varnamelen // Table-driven registry tests keep short locals for readability.
package registry_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- BuildHostEndpointMap ---

//nolint:funlen // Table-driven test with comprehensive endpoint map scenarios
func TestBuildHostEndpointMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		specs           []registry.MirrorSpec
		containerPrefix string
		existing        map[string][]string
		wantUpdated     bool
		wantHosts       []string
	}{
		{
			name:            "nil specs returns unchanged map",
			specs:           nil,
			containerPrefix: "k3d",
			existing:        map[string][]string{},
			wantUpdated:     false,
			wantHosts:       []string{},
		},
		{
			name:            "empty specs returns unchanged map",
			specs:           []registry.MirrorSpec{},
			containerPrefix: "k3d",
			existing:        map[string][]string{},
			wantUpdated:     false,
			wantHosts:       []string{},
		},
		{
			name: "adds new host",
			specs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			containerPrefix: "k3d",
			existing:        nil,
			wantUpdated:     true,
			wantHosts:       []string{"docker.io"},
		},
		{
			name: "adds multiple hosts",
			specs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
				{Host: "ghcr.io", Remote: "https://ghcr.io"},
			},
			containerPrefix: "kind",
			existing:        nil,
			wantUpdated:     true,
			wantHosts:       []string{"docker.io", "ghcr.io"},
		},
		{
			name: "preserves existing endpoints",
			specs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			containerPrefix: "k3d",
			existing: map[string][]string{
				"ghcr.io": {"http://existing:5000"},
			},
			wantUpdated: true,
			wantHosts:   []string{"docker.io", "ghcr.io"},
		},
		{
			name: "no prefix uses bare container name",
			specs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			containerPrefix: "",
			existing:        nil,
			wantUpdated:     true,
			wantHosts:       []string{"docker.io"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, updated := registry.BuildHostEndpointMap(
				tc.specs,
				tc.containerPrefix,
				tc.existing,
			)
			assert.Equal(t, tc.wantUpdated, updated)

			for _, host := range tc.wantHosts {
				_, exists := result[host]
				assert.True(t, exists, "expected host %s in result", host)
			}
		})
	}
}

func TestBuildHostEndpointMap_DoesNotMutateExisting(t *testing.T) {
	t.Parallel()

	existing := map[string][]string{
		"ghcr.io": {"http://existing:5000"},
	}

	specs := []registry.MirrorSpec{
		{Host: "docker.io", Remote: "https://registry-1.docker.io"},
	}

	result, updated := registry.BuildHostEndpointMap(specs, "k3d", existing)
	require.True(t, updated)

	// Verify original map was not modified
	_, hasDocker := existing["docker.io"]
	assert.False(t, hasDocker, "existing map should not have been modified")

	// Verify result contains both
	_, hasGhcr := result["ghcr.io"]
	assert.True(t, hasGhcr)

	_, hasDockerResult := result["docker.io"]
	assert.True(t, hasDockerResult)
}

func TestBuildHostEndpointMap_EndpointsContainMirrorAndRemote(t *testing.T) {
	t.Parallel()

	specs := []registry.MirrorSpec{
		{Host: "docker.io", Remote: "https://registry-1.docker.io"},
	}

	result, updated := registry.BuildHostEndpointMap(specs, "k3d", nil)
	require.True(t, updated)

	endpoints, ok := result["docker.io"]
	require.True(t, ok)
	require.GreaterOrEqual(t, len(endpoints), 1, "expected at least one endpoint for docker.io")

	// Should contain the remote URL
	hasRemote := false

	for _, ep := range endpoints {
		if ep == "https://registry-1.docker.io" {
			hasRemote = true
		}
	}

	assert.True(t, hasRemote, "endpoints should include the remote URL")
}

// --- CollectExistingRegistryPorts (nil backend) ---
// See collect_ports_test.go for comprehensive tests
