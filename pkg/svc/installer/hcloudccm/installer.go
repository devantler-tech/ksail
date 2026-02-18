package hcloudccminstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/helmutil"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/hetzner"
)

// ErrHetznerTokenNotSet is returned when the HCLOUD_TOKEN environment variable is not set.
var ErrHetznerTokenNotSet = hetzner.ErrTokenNotSet

// Installer installs or upgrades the Hetzner Cloud Controller Manager.
//
// It embeds helmutil.Base for the Helm lifecycle and adds a pre-install step
// that creates the required Kubernetes secret with the Hetzner Cloud API token.
//
// The cloud controller manager enables LoadBalancer services on Hetzner Cloud
// by provisioning Hetzner Load Balancers and managing their lifecycle.
//
// Prerequisites:
//   - HCLOUD_TOKEN environment variable must be set with a valid Hetzner Cloud API token
//   - The token requires read/write access to Load Balancers
type Installer struct {
	*helmutil.Base

	kubeconfig string
	context    string
}

// NewInstaller creates a new Hetzner Cloud Controller Manager installer instance.
func NewInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *Installer {
	return &Installer{
		Base: helmutil.NewBase(
			"hcloud-ccm",
			client,
			timeout,
			&helm.RepositoryEntry{
				Name: "hcloud",
				URL:  "https://charts.hetzner.cloud",
			},
			&helm.ChartSpec{
				ReleaseName:     "hcloud-cloud-controller-manager",
				ChartName:       "hcloud/hcloud-cloud-controller-manager",
				Namespace:       hetzner.Namespace,
				RepoURL:         "https://charts.hetzner.cloud",
				CreateNamespace: false, // kube-system already exists
				Atomic:          true,
				Wait:            true,
				WaitForJobs:     true,
				Timeout:         timeout,
			},
		),
		kubeconfig: kubeconfig,
		context:    context,
	}
}

// Install creates the required Hetzner Cloud API token secret and then
// installs or upgrades the cloud controller manager via its Helm chart.
func (h *Installer) Install(ctx context.Context) error {
	err := hetzner.InstallWithSecret(ctx, h.Base, h.kubeconfig, h.context, "hcloud-ccm")
	if err != nil {
		return fmt.Errorf("install hcloud-ccm: %w", err)
	}

	return nil
}
