// Package hetzner provides shared utilities for Hetzner Cloud installers.
package hetzner

import (
	"context"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/helmutil"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Namespace is the Kubernetes namespace for Hetzner resources.
	Namespace = "kube-system"
	// SecretName is the name of the secret containing the Hetzner Cloud API token.
	SecretName = "hcloud"
	// TokenEnvVar is the environment variable that holds the Hetzner Cloud API token.
	TokenEnvVar = "HCLOUD_TOKEN"
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

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SecretName,
			Namespace: Namespace,
		},
		StringData: map[string]string{
			"token": token,
		},
	}

	secrets := clientset.CoreV1().Secrets(Namespace)

	_, err = secrets.Get(ctx, SecretName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to get secret: %w", err)
		}

		_, err = secrets.Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create secret: %w", err)
		}
	} else {
		_, err = secrets.Update(ctx, secret, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update secret: %w", err)
		}
	}

	return nil
}

// InstallWithSecret ensures the Hetzner Cloud API token secret exists and then
// installs or upgrades a Helm chart via the given helmutil.Base. The name
// parameter is used in error messages to identify the component.
func InstallWithSecret(
	ctx context.Context,
	base *helmutil.Base,
	kubeconfig, kubeContext, name string,
) error {
	err := EnsureSecret(ctx, kubeconfig, kubeContext)
	if err != nil {
		return fmt.Errorf("failed to create hetzner secret: %w", err)
	}

	err = base.Install(ctx)
	if err != nil {
		return fmt.Errorf("failed to install %s: %w", name, err)
	}

	return nil
}
