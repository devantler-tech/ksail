package hetznercsiinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
)

// ProvidedByLabel is the node label applied by the Hetzner Cloud Controller
// Manager once it has initialized a node (after matching the Kubernetes node
// to a Hetzner Cloud server). The hcloud-csi node driver registers this key
// as a topology segment on start-up, so if the label is missing when the CSI
// node pod registers, the CSI provisioner later records an incomplete
// topology in CSINode and PVCs with allowedTopologies fail to bind.
//
// See: https://github.com/hetznercloud/csi-driver
const ProvidedByLabel = "instance.hetzner.cloud/provided-by"

// waitForCCMNodeLabelsFn is the production node-label wait used by Installer.
// It is held in a package-level variable so that tests can substitute a fake
// without constructing a real clientset.
//
//nolint:gochecknoglobals // test seam for the readiness wait
var waitForCCMNodeLabelsFn = func(
	ctx context.Context,
	kubeconfig, kubeContext string,
	deadline time.Duration,
) error {
	clientset, err := k8s.NewClientset(kubeconfig, kubeContext)
	if err != nil {
		return fmt.Errorf("create kubernetes client for node-label wait: %w", err)
	}

	err = readiness.WaitForAllNodesLabeled(ctx, clientset, ProvidedByLabel, deadline)
	if err != nil {
		return fmt.Errorf(
			"timed out waiting for hcloud-ccm to label all nodes with %q: %w",
			ProvidedByLabel, err,
		)
	}

	return nil
}
