package registry_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager_NilBackend(t *testing.T) {
	t.Parallel()

	manager, err := registry.NewManager(nil)

	require.Error(t, err)
	assert.Nil(t, manager)
	assert.Contains(t, err.Error(), "registry backend is required")
}

func TestNewManager_ValidBackend(t *testing.T) {
	t.Parallel()

	backend := registry.NewMockBackend(t)
	manager, err := registry.NewManager(backend)

	require.NoError(t, err)
	assert.NotNil(t, manager)
}
