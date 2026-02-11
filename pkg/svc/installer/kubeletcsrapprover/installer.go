package kubeletcsrapproverinstaller

import (
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/helmutil"
)

// Installer installs or upgrades kubelet-csr-approver.
//
// It embeds helmutil.Base to provide standard Helm chart lifecycle management.
type Installer struct {
	*helmutil.Base
}

// NewInstaller creates a new kubelet-csr-approver installer instance.
func NewInstaller(
	client helm.Interface,
	timeout time.Duration,
) *Installer {
	return &Installer{
		Base: helmutil.NewBase(
			"kubelet-csr-approver",
			client,
			timeout,
			&helm.RepositoryEntry{
				Name: "kubelet-csr-approver",
				URL:  "https://postfinance.github.io/kubelet-csr-approver",
			},
			&helm.ChartSpec{
				ReleaseName: "kubelet-csr-approver",
				ChartName:   "kubelet-csr-approver/kubelet-csr-approver",
				Namespace:   "kube-system",
				RepoURL:     "https://postfinance.github.io/kubelet-csr-approver",
				Atomic:      true,
				Wait:        true,
				WaitForJobs: true,
				Timeout:     timeout,
				// Use providerRegex to allow CSRs from any provider (DNS/IP SANs)
				// This is safe in local development clusters where kubelet identities are trusted
				ValuesYaml: `providerRegex: ".*"
bypassDnsResolution: true`,
			},
		),
	}
}
