package talosprovisioner_test

import (
	"io"
	"testing"
	"time"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
)

// TestKubernetesUpgradeOptionsAllowsManifestReconciliation catches the
// zero-value timeout that left the Talos SDK only 60 seconds to reconcile
// kube-proxy after a Kubernetes minor-version upgrade.
func TestKubernetesUpgradeOptionsAllowsManifestReconciliation(t *testing.T) {
	t.Parallel()

	options := talosprovisioner.KubernetesUpgradeOptionsForTest(io.Discard)

	assert.GreaterOrEqual(t, options.ReconcileTimeout, 3*time.Minute)
}
