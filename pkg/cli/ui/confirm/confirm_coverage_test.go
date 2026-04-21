package confirm_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/confirm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShowDeletionPreview_Omni(t *testing.T) {
	t.Parallel()

	preview := &confirm.DeletionPreview{
		ClusterName: "omni-cluster",
		Provider:    v1alpha1.ProviderOmni,
		Servers:     []string{"machine-1", "machine-2", "machine-3"},
	}

	var out bytes.Buffer
	confirm.ShowDeletionPreview(&out, preview)

	output := out.String()
	require.Contains(t, output, "The following resources will be deleted")
	require.Contains(t, output, "omni-cluster")
	require.Contains(t, output, "Omni")
	require.Contains(t, output, "Machines:")
	require.Contains(t, output, "machine-1")
	require.Contains(t, output, "machine-2")
	require.Contains(t, output, "machine-3")
	require.Contains(t, output, `Type "yes" to confirm deletion`)
}

func TestShowDeletionPreview_OmniNoMachines(t *testing.T) {
	t.Parallel()

	preview := &confirm.DeletionPreview{
		ClusterName: "omni-empty",
		Provider:    v1alpha1.ProviderOmni,
	}

	var out bytes.Buffer
	confirm.ShowDeletionPreview(&out, preview)

	output := out.String()
	require.Contains(t, output, "omni-empty")
	require.Contains(t, output, "Omni")
	require.NotContains(t, output, "Machines:")
	require.Contains(t, output, `Type "yes" to confirm deletion`)
}

func TestShowDeletionPreview_DockerSharedContainers(t *testing.T) {
	t.Parallel()

	preview := &confirm.DeletionPreview{
		ClusterName:      "docker-cluster",
		Provider:         v1alpha1.ProviderDocker,
		Nodes:            []string{"node-1"},
		SharedContainers: []string{"cloud-provider-kind"},
	}

	var out bytes.Buffer
	confirm.ShowDeletionPreview(&out, preview)

	output := out.String()
	require.Contains(t, output, "docker-cluster")
	require.Contains(t, output, "Docker")
	require.Contains(t, output, "Containers:")
	require.Contains(t, output, "node-1")
	require.Contains(t, output, "Shared containers (last Kind cluster):")
	require.Contains(t, output, "cloud-provider-kind")
}

func TestShowDeletionPreview_HetznerServersOnly(t *testing.T) {
	t.Parallel()

	preview := &confirm.DeletionPreview{
		ClusterName: "hetzner-minimal",
		Provider:    v1alpha1.ProviderHetzner,
		Servers:     []string{"server-1"},
	}

	var out bytes.Buffer
	confirm.ShowDeletionPreview(&out, preview)

	output := out.String()
	require.Contains(t, output, "hetzner-minimal")
	require.Contains(t, output, "Hetzner")
	require.Contains(t, output, "Servers:")
	require.Contains(t, output, "server-1")
	require.NotContains(t, output, "Placement Group:")
	require.NotContains(t, output, "Firewall:")
	require.NotContains(t, output, "Network:")
}

func TestShowDeletionPreview_HetznerNetworkOnly(t *testing.T) {
	t.Parallel()

	preview := &confirm.DeletionPreview{
		ClusterName: "hetzner-net",
		Provider:    v1alpha1.ProviderHetzner,
		Network:     "hetzner-net-network",
	}

	var out bytes.Buffer
	confirm.ShowDeletionPreview(&out, preview)

	output := out.String()
	require.Contains(t, output, "hetzner-net")
	require.Contains(t, output, "Hetzner")
	require.NotContains(t, output, "Servers:")
	require.NotContains(t, output, "Placement Group:")
	require.NotContains(t, output, "Firewall:")
	require.Contains(t, output, "Network: hetzner-net-network")
}

//nolint:paralleltest // Modifies package-level test overrides.
func TestSetStdinReaderForTests_Restore(t *testing.T) {
	// Verify the override and restore mechanism works correctly
	restore1 := confirm.SetStdinReaderForTests(nil)
	require.NotNil(t, restore1)
	restore1()

	// After restore, setting a new reader should work
	restore2 := confirm.SetStdinReaderForTests(nil)
	require.NotNil(t, restore2)
	restore2()
}

//nolint:paralleltest // Modifies package-level test overrides.
func TestSetTTYCheckerForTests_Restore(t *testing.T) {
	// Set a checker
	restore := confirm.SetTTYCheckerForTests(func() bool { return true })

	assert.True(t, confirm.IsTTY())

	// Restore and check the override is gone
	restore()

	// Set the opposite
	restore = confirm.SetTTYCheckerForTests(func() bool { return false })

	assert.False(t, confirm.IsTTY())

	restore()
}
