package kindprovisioner_test

import (
	"testing"

	kindprovisioner "github.com/devantler-tech/ksail-go/pkg/svc/provisioner/cluster/kind"
	"github.com/stretchr/testify/assert"
)

func TestNewDefaultKindProviderAdapter(t *testing.T) {
	t.Parallel()

	adapter := kindprovisioner.NewDefaultKindProviderAdapter()

	assert.NotNil(t, adapter, "adapter should not be nil")
}

func TestDefaultKindProviderAdapterImplementsInterface(t *testing.T) {
	t.Parallel()

	// Verify that DefaultKindProviderAdapter implements KindProvider interface
	var _ kindprovisioner.KindProvider = (*kindprovisioner.DefaultKindProviderAdapter)(nil)
}
