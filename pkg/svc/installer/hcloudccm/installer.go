package hcloudccminstaller

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
	hcloudCCMNamespace = "kube-system"
	hcloudSecretName   = "hcloud"
	hcloudTokenEnvVar  = "HCLOUD_TOKEN"
)

// ErrHetznerTokenNotSet is returned when the HCLOUD_TOKEN environment variable is not set.
var ErrHetznerTokenNotSet = fmt.Errorf("environment variable %s is not set", hcloudTokenEnvVar)

// Installer installs or upgrades the Hetzner Cloud Controller Manager.
//
// It embeds helmutil.Base for the Helm lifecycle and adds a pre-install step
// that creates the required Kubernetes secret with the Hetzner Cloud API token.
//
// The cloud controller manager enables LoadBalancer services on Hetzner Cloud
// by provisioning Hetzner Load Balancers and managing their lifecycle.
//
// Prerequisites:
//   - HCLOUD_TOKEN environment variable must be set with a valid Hetzner Cloud API token
//   - The token requires read/write access to Load Balancers
type Installer struct {
	*helmutil.Base

	kubeconfig string
	context    string
}

// NewInstaller creates a new Hetzner Cloud Controller Manager installer instance.
func NewInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *Installer {
	return &Installer{
		Base: helmutil.NewBase(
			"hcloud-ccm",
			client,
			timeout,
			&helm.RepositoryEntry{
				Name: "hcloud",
				URL:  "https://charts.hetzner.cloud",
			},
			&helm.ChartSpec{
				ReleaseName:     "hcloud-cloud-controller-manager",
				ChartName:       "hcloud/hcloud-cloud-controller-manager",
				Namespace:       hcloudCCMNamespace,
				RepoURL:         "https://charts.hetzner.cloud",
				CreateNamespace: false, // kube-system already exists
				Atomic:          true,
				Wait:            true,
				WaitForJobs:     true,
				Timeout:         timeout,
			},
		),
		kubeconfig: kubeconfig,
		context:    context,
	}
}

// Install creates the required Hetzner Cloud API token secret and then
// installs or upgrades the cloud controller manager via its Helm chart.
func (h *Installer) Install(ctx context.Context) error {
	err := h.createHetznerSecret(ctx)
	if err != nil {
		return fmt.Errorf("failed to create hetzner secret: %w", err)
	}

	err = h.Base.Install(ctx)
	if err != nil {
		return fmt.Errorf("failed to install hcloud-ccm: %w", err)
	}

	return nil
}

// createHetznerSecret creates the required secret containing the Hetzner Cloud API token.
// The secret is named 'hcloud' and is created in the 'kube-system' namespace.
// It reads the token from the HCLOUD_TOKEN environment variable.
//
// This secret is shared with the Hetzner CSI driver if both are installed.
func (h *Installer) createHetznerSecret(ctx context.Context) error {
	// Get the Hetzner Cloud API token from environment
	token := os.Getenv(hcloudTokenEnvVar)
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
			Name:      hcloudSecretName,
			Namespace: hcloudCCMNamespace,
		},
		StringData: map[string]string{
			"token": token,
		},
	}

	// Try to create or update the secret
	_, err = clientset.CoreV1().Secrets(hcloudCCMNamespace).Get(
		ctx,
		hcloudSecretName,
		metav1.GetOptions{},
	)
	if err != nil {
		// Secret doesn't exist, create it
		_, err = clientset.CoreV1().Secrets(hcloudCCMNamespace).Create(
			ctx,
			secret,
			metav1.CreateOptions{},
		)
		if err != nil {
			return fmt.Errorf("failed to create secret: %w", err)
		}
	} else {
		// Secret exists, update it
		_, err = clientset.CoreV1().Secrets(hcloudCCMNamespace).Update(
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
