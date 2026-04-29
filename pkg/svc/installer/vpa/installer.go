package vpainstaller

import (
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
)

// Installer installs or upgrades the Vertical Pod Autoscaler (VPA).
//
// It embeds helmutil.Base to provide standard Helm chart lifecycle management.
type Installer struct {
	*helmutil.Base
}

// NewInstaller creates a new VPA installer instance.
func NewInstaller(
	client helm.Interface,
	timeout time.Duration,
) *Installer {
	return &Installer{
		Base: helmutil.NewBase(
			"vpa",
			client,
			timeout,
			&helm.RepositoryEntry{
				Name: "fairwinds-stable",
				URL:  "https://charts.fairwinds.com/stable",
			},
			&helm.ChartSpec{
				ReleaseName: "vpa",
				ChartName:   "fairwinds-stable/vpa",
				Namespace:   "kube-system",
				Version:     chartVersion(),
				RepoURL:     "https://charts.fairwinds.com/stable",
				Atomic:      true,
				Wait:        true,
				WaitForJobs: true,
				Timeout:     timeout,
			},
		),
	}
}
