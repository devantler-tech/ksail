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
	hetznerCSINamespace = "kube-system"
	hetznerSecretName   = "hcloud"
	hetznerTokenEnvVar  = "HCLOUD_TOKEN"
)

// ErrHetznerTokenNotSet is returned when the HCLOUD_TOKEN environment variable is not set.
var ErrHetznerTokenNotSet = fmt.Errorf("environment variable %s is not set", hetznerTokenEnvVar)

// Installer installs or upgrades the Hetzner Cloud CSI driver.
//
// It embeds helmutil.Base for the Helm lifecycle and adds a pre-install step
// that creates the required Kubernetes secret with the Hetzner Cloud API token.
//
// Prerequisites:
//   - HCLOUD_TOKEN environment variable must be set with a valid Hetzner Cloud API token
type Installer struct {
	*helmutil.Base

	kubeconfig string
	context    string
}

// NewInstaller creates a new Hetzner CSI installer instance.
func NewInstaller(
	client helm.Interface,
	kubeconfig, context string,
	timeout time.Duration,
) *Installer {
	return &Installer{
		Base: helmutil.NewBase(
			"hetzner-csi",
			client,
			timeout,
			&helm.RepositoryEntry{
				Name: "hcloud",
				URL:  "https://charts.hetzner.cloud",
			},
			&helm.ChartSpec{
				ReleaseName:     "hcloud-csi",
				ChartName:       "hcloud/hcloud-csi",
				Namespace:       hetznerCSINamespace,
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
// installs or upgrades the CSI driver via its Helm chart.
func (h *Installer) Install(ctx context.Context) error {
	err := h.createHetznerSecret(ctx)
	if err != nil {
		return fmt.Errorf("failed to create hetzner secret: %w", err)
	}

	err = h.Base.Install(ctx)
	if err != nil {
		return fmt.Errorf("failed to install hetzner csi: %w", err)
	}

	return nil
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
