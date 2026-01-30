package cloudproviderkindinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
)

// CloudProviderKINDInstaller installs or upgrades Cloud Provider KIND.
type CloudProviderKINDInstaller struct {
	kubeconfig string
	context    string
	timeout    time.Duration
	client     helm.Interface
}

// NewCloudProviderKINDInstaller creates a new Cloud Provider KIND installer instance.
func NewCloudProviderKINDInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *CloudProviderKINDInstaller {
	return &CloudProviderKINDInstaller{
		client:     client,
		kubeconfig: kubeconfig,
		context:    context,
		timeout:    timeout,
	}
}

// Install installs or upgrades Cloud Provider KIND via its Helm chart.
func (c *CloudProviderKINDInstaller) Install(ctx context.Context) error {
	err := c.helmInstallOrUpgradeCloudProviderKIND(ctx)
	if err != nil {
		return fmt.Errorf("failed to install cloud-provider-kind: %w", err)
	}

	return nil
}

// --- internals ---

func (c *CloudProviderKINDInstaller) helmInstallOrUpgradeCloudProviderKIND(
	ctx context.Context,
) error {
	repoEntry := &helm.RepositoryEntry{
		Name: "cloud-provider-kind",
		URL:  "https://kubernetes-sigs.github.io/cloud-provider-kind",
	}

	addRepoErr := c.client.AddRepository(ctx, repoEntry, c.timeout)
	if addRepoErr != nil {
		return fmt.Errorf("failed to add cloud-provider-kind repository: %w", addRepoErr)
	}

	spec := &helm.ChartSpec{
		ReleaseName: "cloud-provider-kind",
		ChartName:   "cloud-provider-kind/cloud-provider-kind",
		Namespace:   "kube-system",
		RepoURL:     "https://kubernetes-sigs.github.io/cloud-provider-kind",
		Atomic:      true,
		Wait:        true,
		WaitForJobs: true,
		Timeout:     c.timeout,
	}

	_, err := c.client.InstallOrUpgradeChart(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to install cloud-provider-kind chart: %w", err)
	}

	return nil
}
