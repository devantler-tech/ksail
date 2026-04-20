package argocdinstaller

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/sopsutil"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// SopsAgeSecretName is the name of the Kubernetes secret used for SOPS Age decryption.
const SopsAgeSecretName = sopsutil.SopsAgeSecretName

var errNilClusterCfg = errors.New("clusterCfg is nil")

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
		return fmt.Errorf("resolve SOPS Age key: %w", err)
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

	desired := sopsutil.BuildSopsAgeSecret(argoCDNamespace, ageKey)

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
