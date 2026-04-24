package hetznercsiinstaller

import (
	"context"
	"time"
)

// BuildSecretDataForTest exports buildSecretData for testing.
var BuildSecretDataForTest = buildSecretData //nolint:gochecknoglobals // Standard Go export_test.go pattern.

// SetWaitForCCMNodeLabelsFnForTest replaces the internal CCM label-wait
// function and returns a restore func. It is exposed only to tests.
func SetWaitForCCMNodeLabelsFnForTest(
	fn func(ctx context.Context, kubeconfig, kubeContext string, deadline time.Duration) error,
) func() {
	prev := waitForCCMNodeLabelsFn
	waitForCCMNodeLabelsFn = fn

	return func() { waitForCCMNodeLabelsFn = prev }
}
