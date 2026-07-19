package docker_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestListAllRegistries_IgnoresNetworkMembership is the regression guard for #6286.
//
// A partial teardown can destroy the cluster network while its registry containers keep
// running, and ListRegistriesOnNetwork cannot see those — it inspects each container for
// membership of a network that no longer exists. ListAllRegistries must find them anyway,
// so cleanup has something to act on.
//
// Concretely: NO ContainerInspect call is expected here. If the implementation regressed to
// network-based filtering, the mock would receive an unexpected inspect call and fail.
func TestListAllRegistries_IgnoresNetworkMembership(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "reg1",
				Names:  []string{"/spike-local-registry"},
				Labels: map[string]string{docker.RegistryLabelKey: "spike-local-registry"},
			},
			{
				ID:    "reg2",
				Names: []string{"/somebody-elses-registry"},
				// No KSail label — reported, but marked not-owned so callers can exclude it.
			},
		}, nil).
		Once()

	registries, err := manager.ListAllRegistries(ctx)

	require.NoError(t, err)
	require.Len(t, registries, 2)

	assert.Equal(t, "spike-local-registry", registries[0].Name)
	assert.True(t, registries[0].IsKSailOwned,
		"a container carrying the KSail registry label must be reported as KSail-owned")

	assert.Equal(t, "somebody-elses-registry", registries[1].Name)
	assert.False(t, registries[1].IsKSailOwned,
		"a registry without the KSail label must never be reported as KSail-owned — "+
			"cleanup uses this flag to decide what it is allowed to remove")
}

// TestListAllRegistries_Empty verifies the no-registries case returns an empty result rather
// than an error, so a caller can distinguish "nothing to clean up" from a failed query.
func TestListAllRegistries_Empty(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{}, nil).
		Once()

	registries, err := manager.ListAllRegistries(ctx)

	require.NoError(t, err)
	assert.Empty(t, registries)
}
