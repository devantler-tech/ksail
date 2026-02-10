package hetznercsiinstaller

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/helmutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	hetznerCSIRepoName  = "hcloud"
	hetznerCSIRepoURL   = "https://charts.hetzner.cloud"
	hetznerCSIRelease   = "hcloud-csi"
	hetznerCSINamespace = "kube-system"
	hetznerCSIChartName = "hcloud/hcloud-csi"
	hetznerSecretName   = "hcloud"
	hetznerTokenEnvVar  = "HCLOUD_TOKEN"
)

// ErrHetznerTokenNotSet is returned when the HCLOUD_TOKEN environment variable is not set.
var ErrHetznerTokenNotSet = fmt.Errorf("environment variable %s is not set", hetznerTokenEnvVar)

// Installer installs or upgrades the Hetzner Cloud CSI driver.
//
// It implements installer.Installer semantics (Install/Uninstall) so it can be
// orchestrated by cluster lifecycle flows.
//
// Prerequisites:
//   - HCLOUD_TOKEN environment variable must be set with a valid Hetzner Cloud API token
type Installer struct {
	client     helm.Interface
	kubeconfig string
	context    string
	timeout    time.Duration
}

// NewInstaller creates a new Hetzner CSI installer instance.
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

// Install installs or upgrades the Hetzner Cloud CSI driver via its Helm chart.
// It first creates the required secret containing the Hetzner Cloud API token,
// then installs the CSI driver chart.
func (h *Installer) Install(ctx context.Context) error {
	// Create the secret containing the Hetzner Cloud API token
	err := h.createHetznerSecret(ctx)
	if err != nil {
		return fmt.Errorf("failed to create hetzner secret: %w", err)
	}

	// Install the CSI driver
	return h.helmInstallOrUpgrade(ctx)
}

// Uninstall removes the Helm release for the Hetzner CSI driver.
func (h *Installer) Uninstall(ctx context.Context) error {
	err := h.client.UninstallRelease(ctx, hetznerCSIRelease, hetznerCSINamespace)
	if err != nil {
		return fmt.Errorf("failed to uninstall hetzner-csi release: %w", err)
	}

	return nil
}

// Images returns the container images used by the Hetzner CSI driver.
func (h *Installer) Images(ctx context.Context) ([]string, error) {
	images, err := helmutil.ImagesFromChart(ctx, h.client, h.chartSpec())
	if err != nil {
		return nil, fmt.Errorf("listing images: %w", err)
	}

	return images, nil
}

func (h *Installer) chartSpec() *helm.ChartSpec {
	return &helm.ChartSpec{
		ReleaseName:     hetznerCSIRelease,
		ChartName:       hetznerCSIChartName,
		Namespace:       hetznerCSINamespace,
		RepoURL:         hetznerCSIRepoURL,
		CreateNamespace: false, // kube-system already exists
		Atomic:          true,
		Wait:            true,
		WaitForJobs:     true,
		Timeout:         h.timeout,
	}
}

// createHetznerSecret creates the required secret containing the Hetzner Cloud API token.
// The secret is named 'hcloud' and is created in the 'kube-system' namespace.
// It reads the token from the HCLOUD_TOKEN environment variable.
func (h *Installer) createHetznerSecret(ctx context.Context) error {
	// Get the Hetzner Cloud API token from environment
	token := os.Getenv(hetznerTokenEnvVar)
	if token == "" {
		return ErrHetznerTokenNotSet
	}

	// Create Kubernetes clientset
	clientset, err := k8s.NewClientset(h.kubeconfig, h.context)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Create the secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hetznerSecretName,
			Namespace: hetznerCSINamespace,
		},
		StringData: map[string]string{
			"token": token,
		},
	}

	// Try to create or update the secret
	_, err = clientset.CoreV1().Secrets(hetznerCSINamespace).Get(
		ctx,
		hetznerSecretName,
		metav1.GetOptions{},
	)
	if err != nil {
		// Secret doesn't exist, create it
		_, err = clientset.CoreV1().Secrets(hetznerCSINamespace).Create(
			ctx,
			secret,
			metav1.CreateOptions{},
		)
		if err != nil {
			return fmt.Errorf("failed to create secret: %w", err)
		}
	} else {
		// Secret exists, update it
		_, err = clientset.CoreV1().Secrets(hetznerCSINamespace).Update(
			ctx,
			secret,
			metav1.UpdateOptions{},
		)
		if err != nil {
			return fmt.Errorf("failed to update secret: %w", err)
		}
	}

	return nil
}

func (h *Installer) helmInstallOrUpgrade(ctx context.Context) error {
	repoEntry := &helm.RepositoryEntry{Name: hetznerCSIRepoName, URL: hetznerCSIRepoURL}

	err := h.client.AddRepository(ctx, repoEntry, h.timeout)
	if err != nil {
		return fmt.Errorf("failed to add hetzner CSI repository: %w", err)
	}

	spec := h.chartSpec()

	_, err = h.client.InstallOrUpgradeChart(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to install hetzner-csi chart: %w", err)
	}

	return nil
}
