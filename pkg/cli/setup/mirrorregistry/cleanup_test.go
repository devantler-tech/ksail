package mirrorregistry_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/mirrorregistry"
	"github.com/stretchr/testify/assert"
)

func TestDefaultCleanupDependencies(t *testing.T) {
	t.Parallel()

	deps := mirrorregistry.DefaultCleanupDependencies()

	// Verify default dependencies are initialized
	assert.NotNil(t, deps.DockerInvoker, "DockerInvoker should not be nil")
	assert.NotNil(t, deps.LocalRegistryDeps, "LocalRegistryDeps should not be nil")
}

func TestErrNoRegistriesFound(t *testing.T) {
	t.Parallel()

	// Verify error message
	assert.Equal(t, "no registries found on network", mirrorregistry.ErrNoRegistriesFound.Error())
}
