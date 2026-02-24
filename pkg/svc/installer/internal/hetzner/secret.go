// Package hetzner provides shared utilities for Hetzner Cloud installers.
package hetzner

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/helmutil"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// Namespace is the Kubernetes namespace for Hetzner resources.
	Namespace = "kube-system"
	// SecretName is the name of the secret containing the Hetzner Cloud API token.
	SecretName = "hcloud"
	// TokenEnvVar is the environment variable that holds the Hetzner Cloud API token.
	TokenEnvVar = "HCLOUD_TOKEN"
	// repoName is the shared Helm repository name for all Hetzner charts.
	repoName = "hcloud"
	// repoURL is the shared Helm repository URL for all Hetzner charts.
	repoURL = "https://charts.hetzner.cloud"
)

// ErrTokenNotSet is returned when the HCLOUD_TOKEN environment variable is not set.
var ErrTokenNotSet = fmt.Errorf("environment variable %s is not set", TokenEnvVar)

// EnsureSecret creates or updates the Hetzner Cloud API token secret in
// kube-system. Both the hcloud-ccm and hetzner-csi installers share this
// secret, so the logic lives here to avoid duplication.
func EnsureSecret(ctx context.Context, kubeconfig, kubeContext string) error {
	token := os.Getenv(TokenEnvVar)
	if token == "" {
		return ErrTokenNotSet
	}

	clientset, err := k8s.NewClientset(kubeconfig, kubeContext)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return ensureSecret(ctx, clientset, token)
}

// ensureSecret is the testable core of EnsureSecret. It accepts an injected
// kubernetes.Interface so that unit tests can substitute a fake clientset.
func ensureSecret(ctx context.Context, client kubernetes.Interface, token string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SecretName,
			Namespace: Namespace,
		},
		Data: map[string][]byte{
			"token": []byte(token),
		},
	}

	secrets := client.CoreV1().Secrets(Namespace)

	existingSecret, err := secrets.Get(ctx, SecretName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to get secret: %w", err)
		}

		_, err = secrets.Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create secret: %w", err)
			}

			// Another concurrent installer created the secret between
			// the Get and Create calls. Fetch and update it.
			existingSecret, getErr := secrets.Get(ctx, SecretName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("failed to get existing secret: %w", getErr)
			}

			existingSecret.Data = secret.Data
			existingSecret.StringData = nil

			_, updateErr := secrets.Update(ctx, existingSecret, metav1.UpdateOptions{})
			if updateErr != nil {
				return fmt.Errorf("failed to update secret after concurrent create: %w", updateErr)
			}
		}
	} else {
		// Update the existing secret's data while preserving its metadata
		existingSecret.Data = secret.Data
		existingSecret.StringData = nil

		_, err = secrets.Update(ctx, existingSecret, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update secret: %w", err)
		}
	}

	return nil
}

// InstallWithSecret ensures the Hetzner Cloud API token secret exists and then
// installs or upgrades a Helm chart via the given helmutil.Base.
func InstallWithSecret(
	ctx context.Context,
	base *helmutil.Base,
	kubeconfig, kubeContext string,
) error {
	err := EnsureSecret(ctx, kubeconfig, kubeContext)
	if err != nil {
		return fmt.Errorf("failed to create hetzner secret: %w", err)
	}

	err = base.Install(ctx)
	if err != nil {
		return fmt.Errorf("install chart: %w", err)
	}

	return nil
}

// Installer is a shared Hetzner Cloud installer that embeds helmutil.Base and
// adds a pre-install step to create the required Kubernetes HCLOUD_TOKEN secret.
// Both hcloudccm and hetznercsi installers delegate to this type.
type Installer struct {
	*helmutil.Base

	kubeconfig string
	context    string
	name       string
}

// ChartConfig holds the chart-specific parameters that differ between Hetzner installers.
type ChartConfig struct {
	// Name identifies the component (e.g. "hcloud-ccm", "hetzner-csi").
	Name string
	// ReleaseName is the Helm release name.
	ReleaseName string
	// ChartName is the fully-qualified chart name (e.g. "hcloud/hcloud-csi").
	ChartName string
	// Version is the pinned chart version.
	Version string
}

// NewInstaller creates a standard Hetzner Cloud installer with the shared
// repository, namespace, and Helm flags. Only the chart-specific parameters
// (name, release name, chart name, version) differ between Hetzner installers.
func NewInstaller(
	client helm.Interface,
	kubeconfig, kubeContext string,
	timeout time.Duration,
	cfg ChartConfig,
) *Installer {
	return &Installer{
		Base: helmutil.NewBase(
			cfg.Name,
			client,
			timeout,
			&helm.RepositoryEntry{
				Name: repoName,
				URL:  repoURL,
			},
			&helm.ChartSpec{
				ReleaseName:     cfg.ReleaseName,
				ChartName:       cfg.ChartName,
				Namespace:       Namespace,
				Version:         cfg.Version,
				RepoURL:         repoURL,
				CreateNamespace: false,
				Atomic:          true,
				Wait:            true,
				WaitForJobs:     true,
				Timeout:         timeout,
			},
		),
		kubeconfig: kubeconfig,
		context:    kubeContext,
		name:       cfg.Name,
	}
}

// Install creates the required Hetzner Cloud API token secret and then
// installs or upgrades the component via its Helm chart.
func (h *Installer) Install(ctx context.Context) error {
	err := InstallWithSecret(ctx, h.Base, h.kubeconfig, h.context)
	if err != nil {
		return fmt.Errorf("install %s: %w", h.name, err)
	}

	return nil
}
