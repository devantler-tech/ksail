package metricsserverinstaller

import (
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
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
	return NewInstallerWithDistribution(client, timeout, "", false)
}

// NewInstallerWithDistribution creates a new metrics-server installer instance
// with distribution-specific configuration.
//
// VCluster virtualizes the kubelet API with self-signed certificates, so
// --kubelet-insecure-tls is required for metrics-server to scrape metrics.
//
// --authentication-tolerate-lookup-failure is always set to prevent metrics-server
// from panicking when the API server is transiently unreachable at startup (e.g. Cilium
// eBPF service map not yet fully programmed for the pod's network namespace).
//
// When haEnabled is true the chart is configured with HA defaults
// (replicas, PDB, topology spread).
func NewInstallerWithDistribution(
	client helm.Interface,
	timeout time.Duration,
	distribution v1alpha1.Distribution,
	haEnabled bool,
) *Installer {
	valuesYaml := buildValuesYaml(distribution, haEnabled)

	var setValues map[string]string
	if haEnabled {
		setValues = map[string]string{
			"replicas":                         "2",
			"podDisruptionBudget.enabled":      "true",
			"podDisruptionBudget.minAvailable": "1",
		}
	}

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
				SetValues:   setValues,
				ValuesYaml:  valuesYaml,
			},
		),
	}
}

// buildValuesYaml returns the Helm values YAML for metrics-server,
// adjusted for the target distribution and HA configuration.
func buildValuesYaml(distribution v1alpha1.Distribution, haEnabled bool) string {
	// Use InternalIP for node communication in local development clusters.
	// Secure TLS is enabled by default - kubelet-csr-approver handles certificate approval.
	// Tolerate transient auth lookup failures so metrics-server retries rather than
	// panicking when the API server ClusterIP is briefly unreachable at pod startup.
	base := `args:
  - --kubelet-preferred-address-types=InternalIP
  - --authentication-tolerate-lookup-failure=true`

	// VCluster virtualizes the kubelet API behind its own proxy, which uses
	// self-signed TLS certificates that metrics-server cannot verify.
	if distribution == v1alpha1.DistributionVCluster {
		base += "\n  - --kubelet-insecure-tls"
	}

	if haEnabled {
		base += `
topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: kubernetes.io/hostname
    whenUnsatisfiable: ScheduleAnyway
    labelSelector:
      matchLabels:
        app.kubernetes.io/name: metrics-server`
	}

	return base
}
