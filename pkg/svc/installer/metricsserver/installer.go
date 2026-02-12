package metricsserverinstaller

import (
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/helmutil"
)

// Installer installs or upgrades metrics-server.
//
// It embeds helmutil.Base to provide standard Helm chart lifecycle management.
type Installer struct {
	*helmutil.Base
}

// NewInstaller creates a new metrics-server installer instance.
func NewInstaller(
	client helm.Interface,
	timeout time.Duration,
) *Installer {
	return &Installer{
		Base: helmutil.NewBase(
			"metrics-server",
			client,
			timeout,
			&helm.RepositoryEntry{
				Name: "metrics-server",
				URL:  "https://kubernetes-sigs.github.io/metrics-server/",
			},
			&helm.ChartSpec{
				ReleaseName: "metrics-server",
				ChartName:   "metrics-server/metrics-server",
				Namespace:   "kube-system",
				RepoURL:     "https://kubernetes-sigs.github.io/metrics-server/",
				Atomic:      true,
				Wait:        true,
				WaitForJobs: true,
				Timeout:     timeout,
				// Use InternalIP for node communication in local development clusters.
				// Secure TLS is enabled by default - kubelet-csr-approver handles certificate approval.
				ValuesYaml: `args:
  - --kubelet-preferred-address-types=InternalIP`,
			},
		),
	}
}
