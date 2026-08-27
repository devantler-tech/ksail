package talosprovisioner

import "time"

// KubernetesUpgradeReconcileTimeoutForTest exposes the Talos SDK manifest
// reconciliation budget to external-package regression tests.
func KubernetesUpgradeReconcileTimeoutForTest() time.Duration {
	return kubernetesUpgradeReconcileTimeout
}
