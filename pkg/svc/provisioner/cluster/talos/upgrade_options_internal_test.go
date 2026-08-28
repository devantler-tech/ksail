package talosprovisioner

import (
	"testing"
	"time"

	"github.com/siderolabs/go-kubernetes/kubernetes/ssa"
	"github.com/stretchr/testify/assert"
)

func TestKubernetesUpgradeOptionsMatchTalosctlDefaults(t *testing.T) {
	t.Parallel()

	options := kubernetesUpgradeOptions(nil)

	assert.Equal(t, 5*time.Minute, options.ReconcileTimeout,
		"library callers must supply the reconciliation default that talosctl normally wires")
	assert.Equal(t, ssa.InventoryPolicyAdoptIfNoInventory, options.InventoryPolicy,
		"library callers must supply the inventory default that talosctl normally wires")
}
