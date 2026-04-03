package argocdinstaller

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/sopsutil"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// SopsAgeSecretName is the name of the Kubernetes secret used for SOPS Age decryption.
	SopsAgeSecretName = "sops-age"
	// sopsAgeKeyField is the data key within the secret that holds the Age private key.
	sopsAgeKeyField = "sops.agekey"
)

var errNilClusterCfg = fmt.Errorf("clusterCfg is nil")

// EnsureSopsAgeSecret creates or updates the sops-age secret in the argocd namespace
// if SOPS is enabled and an Age key is available.
func EnsureSopsAgeSecret(
	ctx context.Context,
	kubeconfig string,
	clusterCfg *v1alpha1.Cluster,
) error {
	if ctx == nil {
		return errNilContext
	}

	if clusterCfg == nil {
		return errNilClusterCfg
	}

	ageKey, err := sopsutil.ResolveEnabledAgeKey(
		clusterCfg.Spec.Cluster.SOPS,
	)
	if err != nil {
		return err
	}

	if ageKey == "" {
		return nil
	}

	restConfig, err := k8s.BuildRESTConfig(kubeconfig, "")
	if err != nil {
		return fmt.Errorf(
			"build REST config for sops-age secret: %w", err,
		)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf(
			"create kubernetes client for sops-age secret: %w", err,
		)
	}

	return upsertSopsAgeSecret(ctx, clientset, ageKey)
}

// upsertSopsAgeSecret creates or updates the sops-age secret in the argocd namespace.
func upsertSopsAgeSecret(ctx context.Context, clientset kubernetes.Interface, ageKey string) error {
	secretsClient := clientset.CoreV1().Secrets(argoCDNamespace)

	desired := buildSopsAgeSecretObj(ageKey)

	existing, err := secretsClient.Get(ctx, SopsAgeSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, createErr := secretsClient.Create(ctx, desired, metav1.CreateOptions{})
		if createErr != nil {
			return fmt.Errorf("create sops-age secret in %s: %w", argoCDNamespace, createErr)
		}

		return nil
	}

	if err != nil {
		return fmt.Errorf("get sops-age secret in %s: %w", argoCDNamespace, err)
	}

	existing.Data = desired.Data
	existing.Labels = desired.Labels
	existing.Type = desired.Type

	_, updateErr := secretsClient.Update(ctx, existing, metav1.UpdateOptions{})
	if updateErr != nil {
		return fmt.Errorf("update sops-age secret in %s: %w", argoCDNamespace, updateErr)
	}

	return nil
}

// buildSopsAgeSecretObj constructs the Kubernetes Secret for SOPS Age decryption in the argocd namespace.
func buildSopsAgeSecretObj(ageKey string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SopsAgeSecretName,
			Namespace: argoCDNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "ksail",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			sopsAgeKeyField: []byte(ageKey),
		},
	}
}
