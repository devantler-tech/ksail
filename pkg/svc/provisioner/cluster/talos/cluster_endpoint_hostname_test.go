package talosprovisioner_test

import (
	"testing"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	taloscontainer "github.com/siderolabs/talos/pkg/machinery/config/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClusterEndpointHostname checks that a config without a Kubernetes cluster
// config or endpoint yields "" (so callers fall back) instead of panicking: the
// Talos 1.14 accessors return nil for a v1alpha1 config with no endpoint and
// for a config with no KubeClusterConfig document.
func TestClusterEndpointHostname(t *testing.T) {
	t.Parallel()

	emptyContainer, err := taloscontainer.New()
	require.NoError(t, err)

	configs, err := talosconfigmanager.NewDefaultConfigsWithVersionContractAndPatches(
		"", talosconfig.TalosVersion1_14, nil,
	)
	require.NoError(t, err)

	withEndpoint, err := configs.WithEndpoint("192.0.2.10")
	require.NoError(t, err)

	for _, testCase := range []struct {
		name     string
		config   talosconfig.Provider
		expected string
	}{
		{name: "nil provider", config: nil, expected: ""},
		{name: "no documents", config: emptyContainer, expected: ""},
		{
			name:     "v1alpha1 without cluster endpoint",
			config:   sysctlsConfig(map[string]string{sysctlRmemMax: "1"}),
			expected: "",
		},
		{name: "1.14 endpoint", config: withEndpoint.ControlPlane(), expected: "192.0.2.10"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected,
				talosprovisioner.ClusterEndpointHostnameForTest(testCase.config))
		})
	}
}
