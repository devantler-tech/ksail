package registry_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // reads shared backendFactoryOverride global
func TestGetBackendFactory_ReturnsDefaultWhenNoOverride(t *testing.T) {
	factory := registry.GetBackendFactory()

	require.NotNil(t, factory, "GetBackendFactory should return a non-nil factory")
}

//nolint:paralleltest // mutates shared backendFactoryOverride global
func TestSetBackendFactoryForTests_OverridesFactory(t *testing.T) {
	called := false
	mockFactory := func(_ client.APIClient) (registry.Backend, error) {
		called = true

		return registry.NewMockBackend(t), nil
	}

	cleanup := registry.SetBackendFactoryForTests(mockFactory)
	defer cleanup()

	factory := registry.GetBackendFactory()
	require.NotNil(t, factory)

	_, err := factory(nil)
	require.NoError(t, err)
	assert.True(t, called, "override factory should have been called")
}

//nolint:paralleltest // mutates shared backendFactoryOverride global
func TestSetBackendFactoryForTests_CleanupRestoresOriginal(t *testing.T) {
	originalFactory := registry.GetBackendFactory()

	mockFactory := func(_ client.APIClient) (registry.Backend, error) {
		return registry.NewMockBackend(t), nil
	}

	cleanup := registry.SetBackendFactoryForTests(mockFactory)
	cleanup()

	restoredFactory := registry.GetBackendFactory()

	// After cleanup, the factory should be the same function as the original.
	// We can't compare funcs directly, so we verify it's not the mock by calling it with nil.
	// The default factory will fail because nil docker client, while mock succeeds.
	_, err := restoredFactory(nil)
	require.Error(
		t, err,
		"restored factory should be the original DefaultBackendFactory which rejects nil client",
	)

	// Verify the original also fails the same way
	_, origErr := originalFactory(nil)
	assert.Error(t, origErr, "original factory should also reject nil client")
}

//nolint:paralleltest // mutates shared backendFactoryOverride global
func TestSetBackendFactoryForTests_NestedOverrides(t *testing.T) {
	firstCalled := false
	secondCalled := false

	firstFactory := func(_ client.APIClient) (registry.Backend, error) {
		firstCalled = true

		return registry.NewMockBackend(t), nil
	}

	secondFactory := func(_ client.APIClient) (registry.Backend, error) {
		secondCalled = true

		return registry.NewMockBackend(t), nil
	}

	cleanup1 := registry.SetBackendFactoryForTests(firstFactory)

	cleanup2 := registry.SetBackendFactoryForTests(secondFactory)

	// Should use second override
	factory := registry.GetBackendFactory()
	_, err := factory(nil)
	require.NoError(t, err)
	assert.True(t, secondCalled, "second override should be active")
	assert.False(t, firstCalled, "first override should not be called yet")

	// Restore to first override
	cleanup2()

	secondCalled = false

	factory = registry.GetBackendFactory()
	_, err = factory(nil)
	require.NoError(t, err)
	assert.True(t, firstCalled, "first override should be active after cleanup2")
	assert.False(t, secondCalled, "second override should not be called after cleanup2")

	// Restore to original
	cleanup1()
}

func TestDefaultBackendFactory_RejectsNilClient(t *testing.T) {
	t.Parallel()

	_, err := registry.DefaultBackendFactory(nil)

	assert.Error(t, err, "DefaultBackendFactory should return an error for nil client")
}
