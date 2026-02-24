package vclusterprovisioner_test

import (
"context"
"errors"
"io"
"testing"

dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
vclusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/vcluster"
"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
"github.com/docker/docker/api/types/container"
"github.com/stretchr/testify/assert"
testifymock "github.com/stretchr/testify/mock"
"github.com/stretchr/testify/require"
)

var (
errMockContainerList = errors.New("container list failed")
)

func TestSetupRegistries_EmptySpecs(t *testing.T) {
t.Parallel()

mockClient := dockerclient.NewMockAPIClient(t)

err := vclusterprovisioner.SetupRegistries(
context.Background(),
"test-cluster",
mockClient,
[]registry.MirrorSpec{},
io.Discard,
)

require.NoError(t, err, "SetupRegistries() with empty specs should not error")
mockClient.AssertExpectations(t)
}

func TestConnectRegistriesToNetwork_EmptySpecs(t *testing.T) {
t.Parallel()

mockClient := dockerclient.NewMockAPIClient(t)

err := vclusterprovisioner.ConnectRegistriesToNetwork(
context.Background(),
[]registry.MirrorSpec{},
"test-cluster",
mockClient,
io.Discard,
)

require.NoError(t, err, "ConnectRegistriesToNetwork() with empty specs should not error")
mockClient.AssertExpectations(t)
}

func TestCleanupRegistries_EmptySpecs(t *testing.T) {
t.Parallel()

mockClient := dockerclient.NewMockAPIClient(t)

err := vclusterprovisioner.CleanupRegistries(
context.Background(),
[]registry.MirrorSpec{},
"test-cluster",
mockClient,
false,
)

require.NoError(t, err, "CleanupRegistries() with empty specs should not error")
mockClient.AssertExpectations(t)
}

func TestConfigureContainerdRegistryMirrors_EmptySpecs(t *testing.T) {
t.Parallel()

mockClient := dockerclient.NewMockAPIClient(t)

err := vclusterprovisioner.ConfigureContainerdRegistryMirrors(
context.Background(),
"test-cluster",
[]registry.MirrorSpec{},
mockClient,
io.Discard,
)

require.NoError(t, err, "ConfigureContainerdRegistryMirrors() with empty specs should not error")
mockClient.AssertExpectations(t)
}

func TestConfigureContainerdRegistryMirrors_NoNodes(t *testing.T) {
t.Parallel()

mockClient := dockerclient.NewMockAPIClient(t)

// Mock empty container list (no VCluster nodes)
mockClient.On("ContainerList", testifymock.Anything, testifymock.Anything).
Return([]container.Summary{}, nil)

specs := []registry.MirrorSpec{
{
Host:   "docker.io",
Remote: "https://registry-1.docker.io",
},
}

err := vclusterprovisioner.ConfigureContainerdRegistryMirrors(
context.Background(),
"test-cluster",
specs,
mockClient,
io.Discard,
)

require.Error(t, err, "ConfigureContainerdRegistryMirrors() should error when no nodes found")
assert.ErrorIs(t, err, vclusterprovisioner.ErrNoVClusterNodes, "should return ErrNoVClusterNodes")
mockClient.AssertExpectations(t)
}

func TestConfigureContainerdRegistryMirrors_ListError(t *testing.T) {
t.Parallel()

mockClient := dockerclient.NewMockAPIClient(t)

// Mock container list error
mockClient.On("ContainerList", testifymock.Anything, testifymock.Anything).
Return([]container.Summary(nil), errMockContainerList)

specs := []registry.MirrorSpec{
{
Host:   "docker.io",
Remote: "https://registry-1.docker.io",
},
}

err := vclusterprovisioner.ConfigureContainerdRegistryMirrors(
context.Background(),
"test-cluster",
specs,
mockClient,
io.Discard,
)

require.Error(t, err, "ConfigureContainerdRegistryMirrors() should error on list failure")
assert.ErrorContains(t, err, "container list failed", "error should contain list error")
mockClient.AssertExpectations(t)
}
