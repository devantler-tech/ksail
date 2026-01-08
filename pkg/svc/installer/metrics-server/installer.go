package metricsserverinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
)

// MetricsServerInstaller installs or upgrades metrics-server.
type MetricsServerInstaller struct {
	kubeconfig string
	context    string
	timeout    time.Duration
	client     helm.Interface
}

// NewMetricsServerInstaller creates a new metrics-server installer instance.
func NewMetricsServerInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *MetricsServerInstaller {
	return &MetricsServerInstaller{
		client:     client,
		kubeconfig: kubeconfig,
		context:    context,
		timeout:    timeout,
	}
}

// Install installs or upgrades metrics-server via its Helm chart.
func (m *MetricsServerInstaller) Install(ctx context.Context) error {
	err := m.helmInstallOrUpgradeMetricsServer(ctx)
	if err != nil {
		return fmt.Errorf("failed to install metrics-server: %w", err)
	}

	return nil
}

// --- internals ---

func (m *MetricsServerInstaller) helmInstallOrUpgradeMetricsServer(ctx context.Context) error {
	repoEntry := &helm.RepositoryEntry{
		Name: "metrics-server",
		URL:  "https://kubernetes-sigs.github.io/metrics-server/",
	}

	addRepoErr := m.client.AddRepository(ctx, repoEntry, m.timeout)
	if addRepoErr != nil {
		return fmt.Errorf("failed to add metrics-server repository: %w", addRepoErr)
	}

	spec := &helm.ChartSpec{
		ReleaseName: "metrics-server",
		ChartName:   "metrics-server/metrics-server",
		Namespace:   "kube-system",
		RepoURL:     "https://kubernetes-sigs.github.io/metrics-server/",
		Atomic:      true,
		Wait:        true,
		WaitForJobs: true,
		Timeout:     m.timeout,
		// Use InternalIP for node communication in local development clusters.
		// Secure TLS is enabled by default - kubelet-csr-approver handles certificate approval.
		ValuesYaml: `args:
  - --kubelet-preferred-address-types=InternalIP`,
	}

	_, err := m.client.InstallOrUpgradeChart(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to install metrics-server chart: %w", err)
	}

	return nil
}
