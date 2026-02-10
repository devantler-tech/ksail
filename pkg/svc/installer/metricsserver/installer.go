package metricsserverinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/helmutil"
)

const (
	metricsServerRepoName  = "metrics-server"
	metricsServerRepoURL   = "https://kubernetes-sigs.github.io/metrics-server/"
	metricsServerRelease   = "metrics-server"
	metricsServerNamespace = "kube-system"
	metricsServerChartName = "metrics-server/metrics-server"
)

// Installer installs or upgrades metrics-server.
type Installer struct {
	kubeconfig string
	context    string
	timeout    time.Duration
	client     helm.Interface
}

// NewInstaller creates a new metrics-server installer instance.
func NewInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *Installer {
	return &Installer{
		client:     client,
		kubeconfig: kubeconfig,
		context:    context,
		timeout:    timeout,
	}
}

// Install installs or upgrades metrics-server via its Helm chart.
func (m *Installer) Install(ctx context.Context) error {
	err := m.helmInstallOrUpgrade(ctx)
	if err != nil {
		return fmt.Errorf("failed to install metrics-server: %w", err)
	}

	return nil
}

// Uninstall removes the Helm release for metrics-server.
func (m *Installer) Uninstall(ctx context.Context) error {
	err := m.client.UninstallRelease(ctx, metricsServerRelease, metricsServerNamespace)
	if err != nil {
		return fmt.Errorf("failed to uninstall metrics-server release: %w", err)
	}

	return nil
}

// Images returns the container images used by metrics-server.
func (m *Installer) Images(ctx context.Context) ([]string, error) {
	images, err := helmutil.ImagesFromChart(ctx, m.client, m.chartSpec())
	if err != nil {
		return nil, fmt.Errorf("listing images: %w", err)
	}

	return images, nil
}

func (m *Installer) chartSpec() *helm.ChartSpec {
	return &helm.ChartSpec{
		ReleaseName: metricsServerRelease,
		ChartName:   metricsServerChartName,
		Namespace:   metricsServerNamespace,
		RepoURL:     metricsServerRepoURL,
		Atomic:      true,
		Wait:        true,
		WaitForJobs: true,
		Timeout:     m.timeout,
		// Use InternalIP for node communication in local development clusters.
		// Secure TLS is enabled by default - kubelet-csr-approver handles certificate approval.
		ValuesYaml: `args:
  - --kubelet-preferred-address-types=InternalIP`,
	}
}

// --- internals ---

func (m *Installer) helmInstallOrUpgrade(ctx context.Context) error {
	repoEntry := &helm.RepositoryEntry{
		Name: metricsServerRepoName,
		URL:  metricsServerRepoURL,
	}

	addRepoErr := m.client.AddRepository(ctx, repoEntry, m.timeout)
	if addRepoErr != nil {
		return fmt.Errorf("failed to add metrics-server repository: %w", addRepoErr)
	}

	spec := m.chartSpec()

	_, err := m.client.InstallOrUpgradeChart(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to install metrics-server chart: %w", err)
	}

	return nil
}
