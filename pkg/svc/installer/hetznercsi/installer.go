package hetznercsiinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/hetzner"
)

// ErrHetznerTokenNotSet is returned when the HCLOUD_TOKEN environment variable is not set.
var ErrHetznerTokenNotSet = hetzner.ErrTokenNotSet

// Installer installs or upgrades the Hetzner Cloud CSI driver.
//
// It delegates to [hetzner.Installer] which handles the shared Hetzner
// lifecycle (HCLOUD_TOKEN secret + Helm chart install), and adds a pre-install
// gate that waits for the Hetzner Cloud Controller Manager (hcloud-ccm) to
// label every node with [ProvidedByLabel]. This ordering is critical because
// the hcloud-csi node driver registers topology segments from node labels at
// start-up; if the CCM-applied label is missing at that moment, the CSI
// provisioner later reports incomplete topology and PVCs with
// allowedTopologies (e.g. from `global.enableProvidedByTopology: true`) fail
// to bind. See issue #2... for details.
//
// Prerequisites:
//   - HCLOUD_TOKEN environment variable must be set with a valid Hetzner Cloud API token
//   - hcloud-ccm must be installed (it runs in parallel; the pre-install gate
//     blocks CSI until CCM has finished initializing nodes)
type Installer struct {
	*hetzner.Installer

	kubeconfig  string
	kubeContext string
	waitTimeout time.Duration
}

// NewInstaller creates a new Hetzner CSI installer instance.
//
// The networkName parameter specifies the Hetzner Cloud private network name.
// When set, it is stored in the shared "hcloud" Kubernetes secret (key "network")
// alongside the HCLOUD_TOKEN. Storing the network name symmetrically from both
// the CCM and CSI installers ensures the secret is always populated with the
// correct network regardless of which component installs first (or alone),
// which is required by GitOps-managed CCM HelmReleases that read HCLOUD_NETWORK
// from this secret via the chart's default valueFrom.secretKeyRef.
//
// An empty networkName leaves the "network" key untouched so concurrent
// installers (e.g. CCM) don't overwrite each other's values.
func NewInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
	networkName string,
) *Installer {
	base := hetzner.NewInstaller(client, kubeconfig, context, timeout, hetzner.ChartConfig{
		Name:        "hetzner-csi",
		ReleaseName: "hcloud-csi",
		ChartName:   "hcloud/hcloud-csi",
		Version:     chartVersion(),
		SecretData:  buildSecretData(networkName),
	})

	return &Installer{
		Installer:   base,
		kubeconfig:  kubeconfig,
		kubeContext: context,
		waitTimeout: timeout,
	}
}

// Install waits for hcloud-ccm to initialize all nodes (i.e. apply the
// [ProvidedByLabel] label) and then installs the Hetzner CSI driver.
//
// The wait is required because CSI and CCM install concurrently during the
// infrastructure phase. Without this gate, the csi-node DaemonSet can start
// before CCM has labeled nodes, causing the driver to register an incomplete
// topology and breaking PVC provisioning for StorageClasses that rely on
// `csi.hetzner.cloud/location` + `instance.hetzner.cloud/provided-by`.
func (h *Installer) Install(ctx context.Context) error {
	err := waitForCCMNodeLabelsFn(ctx, h.kubeconfig, h.kubeContext, h.waitTimeout)
	if err != nil {
		return fmt.Errorf("wait for hcloud-ccm node initialization: %w", err)
	}

	return h.Installer.Install(ctx)
}

// buildSecretData returns extra key-value pairs for the shared "hcloud" secret.
// When networkName is set, it includes the "network" key so that consumers of
// the secret (the hcloud-ccm chart's default valueFrom.secretKeyRef, and any
// GitOps-managed CCM/CSI HelmReleases) can read HCLOUD_NETWORK from it.
func buildSecretData(networkName string) map[string][]byte {
	if networkName == "" {
		return nil
	}

	return map[string][]byte{
		"network": []byte(networkName),
	}
}
