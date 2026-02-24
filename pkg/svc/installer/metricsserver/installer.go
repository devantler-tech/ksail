package metricsserverinstaller

import (
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
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
	return NewInstallerWithDistribution(client, timeout, "")
}

// NewInstallerWithDistribution creates a new metrics-server installer instance
// with distribution-specific configuration.
//
// VCluster virtualizes the kubelet API with self-signed certificates, so
// --kubelet-insecure-tls is required for metrics-server to scrape metrics.
func NewInstallerWithDistribution(
	client helm.Interface,
	timeout time.Duration,
	distribution v1alpha1.Distribution,
) *Installer {
	valuesYaml := buildValuesYaml(distribution)

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
				Version:     chartVersion(),
				RepoURL:     "https://kubernetes-sigs.github.io/metrics-server/",
				Atomic:      true,
				Wait:        true,
				WaitForJobs: true,
				Timeout:     timeout,
				ValuesYaml:  valuesYaml,
			},
		),
	}
}

// buildValuesYaml returns the Helm values YAML for metrics-server,
// adjusted for the target distribution.
func buildValuesYaml(distribution v1alpha1.Distribution) string {
	// Use InternalIP for node communication in local development clusters.
	// Secure TLS is enabled by default - kubelet-csr-approver handles certificate approval.
	base := `args:
  - --kubelet-preferred-address-types=InternalIP`

	// VCluster virtualizes the kubelet API behind its own proxy, which uses
	// self-signed TLS certificates that metrics-server cannot verify.
	if distribution == v1alpha1.DistributionVCluster {
		base += "\n  - --kubelet-insecure-tls"
	}

	return base
}
