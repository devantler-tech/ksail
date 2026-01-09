package kubeletcsrapproverinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
)

const (
	repoName  = "kubelet-csr-approver"
	repoURL   = "https://postfinance.github.io/kubelet-csr-approver"
	release   = "kubelet-csr-approver"
	namespace = "kube-system"
	chartName = "kubelet-csr-approver/kubelet-csr-approver"
)

// KubeletCSRApproverInstaller installs or upgrades kubelet-csr-approver.
//
// It implements installer.Installer semantics (Install/Uninstall) so it can be
// orchestrated by cluster lifecycle flows.
type KubeletCSRApproverInstaller struct {
	client  helm.Interface
	timeout time.Duration
}

// NewKubeletCSRApproverInstaller creates a new kubelet-csr-approver installer instance.
func NewKubeletCSRApproverInstaller(
	client helm.Interface,
	timeout time.Duration,
) *KubeletCSRApproverInstaller {
	return &KubeletCSRApproverInstaller{client: client, timeout: timeout}
}

// Install installs or upgrades kubelet-csr-approver via its Helm chart.
func (k *KubeletCSRApproverInstaller) Install(ctx context.Context) error {
	return k.helmInstallOrUpgradeKubeletCSRApprover(ctx)
}

// Uninstall removes the Helm release for kubelet-csr-approver.
func (k *KubeletCSRApproverInstaller) Uninstall(ctx context.Context) error {
	err := k.client.UninstallRelease(ctx, release, namespace)
	if err != nil {
		return fmt.Errorf("failed to uninstall kubelet-csr-approver release: %w", err)
	}

	return nil
}

func (k *KubeletCSRApproverInstaller) helmInstallOrUpgradeKubeletCSRApprover(
	ctx context.Context,
) error {
	repoEntry := &helm.RepositoryEntry{Name: repoName, URL: repoURL}

	err := k.client.AddRepository(ctx, repoEntry, k.timeout)
	if err != nil {
		return fmt.Errorf("failed to add kubelet-csr-approver repository: %w", err)
	}

	spec := &helm.ChartSpec{
		ReleaseName: release,
		ChartName:   chartName,
		Namespace:   namespace,
		RepoURL:     repoURL,
		Atomic:      true,
		Wait:        true,
		WaitForJobs: true,
		Timeout:     k.timeout,
		// Use providerRegex to allow CSRs from any provider (DNS/IP SANs)
		// This is safe in local development clusters where kubelet identities are trusted
		ValuesYaml: `providerRegex: ".*"
bypassDnsResolution: true`,
	}

	_, err = k.client.InstallOrUpgradeChart(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to install kubelet-csr-approver chart: %w", err)
	}

	return nil
}
