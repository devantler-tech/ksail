package talosprovisioner

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestKubernetesUpgradeOptionsAllowsManifestReconciliation catches the
// zero-value timeout that left the Talos SDK only 60 seconds to reconcile
// kube-proxy after a Kubernetes minor-version upgrade.
func TestKubernetesUpgradeOptionsAllowsManifestReconciliation(t *testing.T) {
	t.Parallel()

	options := kubernetesUpgradeOptions(io.Discard)

	assert.GreaterOrEqual(t, options.ReconcileTimeout, 3*time.Minute)
}
