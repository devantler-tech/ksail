package kindprovisioner_test

import (
	"testing"

	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	"github.com/stretchr/testify/assert"
)

func TestNewDefaultProviderAdapter(t *testing.T) {
	t.Parallel()

	adapter := kindprovisioner.NewDefaultProviderAdapter()

	assert.NotNil(t, adapter, "adapter should not be nil")
}

func TestDefaultProviderAdapterImplementsInterface(t *testing.T) {
	t.Parallel()

	// Verify that DefaultProviderAdapter implements Provider interface
	var _ kindprovisioner.Provider = (*kindprovisioner.DefaultProviderAdapter)(nil)
}
