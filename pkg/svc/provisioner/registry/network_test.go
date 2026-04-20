package registry_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- calculateRegistryIPs ---

func TestCalculateRegistryIPs_EmptyCIDR_ReturnsEmptyStrings(t *testing.T) {
	t.Parallel()

	result := registry.ExportCalculateRegistryIPs("", 3)

	require.Len(t, result, 3)

	for _, ip := range result {
		assert.Empty(t, ip)
	}
}

func TestCalculateRegistryIPs_ZeroCount_ReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	result := registry.ExportCalculateRegistryIPs("10.5.0.0/24", 0)

	assert.Empty(t, result)
}

func TestCalculateRegistryIPs_InvalidCIDR_ReturnsEmptyStrings(t *testing.T) {
	t.Parallel()

	result := registry.ExportCalculateRegistryIPs("not-a-cidr", 2)

	require.Len(t, result, 2)

	for _, ip := range result {
		assert.Empty(t, ip)
	}
}

func TestCalculateRegistryIPs_IPv6CIDR_ReturnsEmptyStrings(t *testing.T) {
	t.Parallel()

	result := registry.ExportCalculateRegistryIPs("2001:db8::/32", 2)

	require.Len(t, result, 2)

	for _, ip := range result {
		assert.Empty(t, ip)
	}
}

func TestCalculateRegistryIPs_SingleRegistry_Returns250(t *testing.T) {
	t.Parallel()

	result := registry.ExportCalculateRegistryIPs("10.5.0.0/24", 1)

	require.Len(t, result, 1)
	assert.Equal(t, "10.5.0.250", result[0])
}

func TestCalculateRegistryIPs_MultipleRegistries_AssignsDescendingIPs(t *testing.T) {
	t.Parallel()

	result := registry.ExportCalculateRegistryIPs("10.5.0.0/24", 3)

	require.Len(t, result, 3)
	assert.Equal(t, "10.5.0.250", result[0])
	assert.Equal(t, "10.5.0.249", result[1])
	assert.Equal(t, "10.5.0.248", result[2])
}

func TestCalculateRegistryIPs_DifferentSubnet_UsesCorrectBase(t *testing.T) {
	t.Parallel()

	result := registry.ExportCalculateRegistryIPs("192.168.1.0/24", 2)

	require.Len(t, result, 2)
	assert.Equal(t, "192.168.1.250", result[0])
	assert.Equal(t, "192.168.1.249", result[1])
}

// --- staticIPAt ---

func TestStaticIPAt_ValidIndex_ReturnsIP(t *testing.T) {
	t.Parallel()

	ips := []string{"10.0.0.250", "10.0.0.249", "10.0.0.248"}

	assert.Equal(t, "10.0.0.250", registry.ExportStaticIPAt(ips, 0))
	assert.Equal(t, "10.0.0.249", registry.ExportStaticIPAt(ips, 1))
	assert.Equal(t, "10.0.0.248", registry.ExportStaticIPAt(ips, 2))
}

func TestStaticIPAt_OutOfBounds_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	ips := []string{"10.0.0.250"}

	assert.Empty(t, registry.ExportStaticIPAt(ips, 1))
	assert.Empty(t, registry.ExportStaticIPAt(ips, 99))
}

func TestStaticIPAt_EmptySlice_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, registry.ExportStaticIPAt(nil, 0))
	assert.Empty(t, registry.ExportStaticIPAt([]string{}, 0))
}

// --- ConnectRegistriesToNetwork guard clauses ---

func TestConnectRegistriesToNetwork_NilClient_ReturnsNil(t *testing.T) {
	t.Parallel()

	registries := []registry.Info{{Name: "test-registry", Host: "localhost"}}
	err := registry.ConnectRegistriesToNetwork(
		context.Background(),
		nil,
		registries,
		"test-network",
		&bytes.Buffer{},
	)

	require.NoError(t, err)
}

func TestConnectRegistriesToNetwork_EmptyRegistries_ReturnsNil(t *testing.T) {
	t.Parallel()

	err := registry.ConnectRegistriesToNetwork(
		context.Background(),
		docker.NewMockAPIClient(t),
		[]registry.Info{},
		"test-network",
		&bytes.Buffer{},
	)

	require.NoError(t, err)
}

func TestConnectRegistriesToNetwork_EmptyNetworkName_ReturnsNil(t *testing.T) {
	t.Parallel()

	registries := []registry.Info{{Name: "test-registry"}}
	err := registry.ConnectRegistriesToNetwork(
		context.Background(),
		docker.NewMockAPIClient(t),
		registries,
		"",
		&bytes.Buffer{},
	)

	require.NoError(t, err)
}

// --- ConnectRegistriesToNetworkWithStaticIPs guard clauses ---

func TestConnectRegistriesToNetworkWithStaticIPs_NilClient_ReturnsEmptyMap(t *testing.T) {
	t.Parallel()

	registries := []registry.Info{{Name: "test-registry"}}
	result, err := registry.ConnectRegistriesToNetworkWithStaticIPs(
		context.Background(),
		nil,
		registries,
		"test-network",
		"10.0.0.0/24",
		&bytes.Buffer{},
	)

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestConnectRegistriesToNetworkWithStaticIPs_EmptyRegistries_ReturnsEmptyMap(t *testing.T) {
	t.Parallel()

	result, err := registry.ConnectRegistriesToNetworkWithStaticIPs(
		context.Background(),
		docker.NewMockAPIClient(t),
		[]registry.Info{},
		"test-network",
		"10.0.0.0/24",
		&bytes.Buffer{},
	)

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestConnectRegistriesToNetworkWithStaticIPs_EmptyNetworkName_ReturnsEmptyMap(t *testing.T) {
	t.Parallel()

	registries := []registry.Info{{Name: "test-registry"}}
	result, err := registry.ConnectRegistriesToNetworkWithStaticIPs(
		context.Background(),
		docker.NewMockAPIClient(t),
		registries,
		"",
		"10.0.0.0/24",
		&bytes.Buffer{},
	)

	require.NoError(t, err)
	assert.Empty(t, result)
}

// --- ConnectMirrorSpecsToNetwork guard clauses ---

func TestConnectMirrorSpecsToNetwork_EmptySpecs_ReturnsNil(t *testing.T) {
	t.Parallel()

	err := registry.ConnectMirrorSpecsToNetwork(
		context.Background(),
		[]registry.MirrorSpec{},
		"my-cluster",
		"test-network",
		nil,
		&bytes.Buffer{},
	)

	require.NoError(t, err)
}

// --- CleanupRegistries guard clauses ---

func TestCleanupRegistries_NilManager_ReturnsNil(t *testing.T) {
	t.Parallel()

	registries := []registry.Info{{Name: "test-registry"}}
	err := registry.CleanupRegistries(
		context.Background(),
		nil,
		registries,
		"my-cluster",
		false,
		"test-network",
		nil,
	)

	require.NoError(t, err)
}

func TestCleanupRegistries_EmptyRegistries_ReturnsNil(t *testing.T) {
	t.Parallel()

	err := registry.CleanupRegistries(
		context.Background(),
		registry.NewMockBackend(t),
		[]registry.Info{},
		"my-cluster",
		false,
		"test-network",
		nil,
	)

	require.NoError(t, err)
}
