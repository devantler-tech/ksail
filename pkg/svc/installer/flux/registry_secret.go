package fluxinstaller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxclient "github.com/devantler-tech/ksail/v7/pkg/client/flux"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ExternalRegistrySecretName is the name of the Kubernetes secret used for external registry authentication.
	// This secret is created by KSail during cluster creation when credentials are configured.
	//nolint:gosec // not credentials, just a secret name constant
	ExternalRegistrySecretName = "ksail-registry-credentials"
)

// ensureExternalRegistrySecret creates registry secret if external registry with credentials.
func ensureExternalRegistrySecret(
	ctx context.Context,
	restConfig *rest.Config,
	clusterCfg *v1alpha1.Cluster,
) error {
	localRegistry := clusterCfg.Spec.Cluster.LocalRegistry
	if !localRegistry.IsExternal() || !localRegistry.HasCredentials() {
		return nil
	}

	err := ensureRegistrySecret(ctx, restConfig, clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create registry secret: %w", err)
	}

	return nil
}

// ensureRegistrySecret creates or updates the docker-registry secret for OCI authentication.
// This secret is used by Flux to pull artifacts from private external registries.
func ensureRegistrySecret(
	ctx context.Context,
	restConfig *rest.Config,
	clusterCfg *v1alpha1.Cluster,
) error {
	k8sClient, err := newCoreV1Client(restConfig)
	if err != nil {
		return err
	}

	secret, err := buildRegistrySecret(clusterCfg)
	if err != nil {
		return err
	}

	return upsertSecret(ctx, k8sClient, secret)
}

// newCoreV1Client creates a Kubernetes client with core/v1 types registered.
func newCoreV1Client(restConfig *rest.Config) (client.Client, error) {
	scheme := runtime.NewScheme()

	err := corev1.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add core scheme: %w", err)
	}

	k8sClient, err := newDynamicClient(restConfig, scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return k8sClient, nil
}

// buildRegistrySecret creates the Secret object for registry authentication.
func buildRegistrySecret(clusterCfg *v1alpha1.Cluster) (*corev1.Secret, error) {
	localRegistry := clusterCfg.Spec.Cluster.LocalRegistry
	parsed := localRegistry.Parse()
	username, password := localRegistry.ResolveCredentials()

	dockerConfig, err := buildDockerConfigJSON(parsed.Host, username, password)
	if err != nil {
		return nil, fmt.Errorf("failed to build docker config: %w", err)
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ExternalRegistrySecretName,
			Namespace: fluxclient.DefaultNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "ksail",
			},
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: dockerConfig,
		},
	}, nil
}

// upsertSecret creates or updates a Kubernetes secret.
func upsertSecret(ctx context.Context, k8sClient client.Client, secret *corev1.Secret) error {
	existing := &corev1.Secret{}
	err := k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), existing)
	secretRef := secret.Namespace + "/" + secret.Name

	if apierrors.IsNotFound(err) {
		createErr := k8sClient.Create(ctx, secret)
		if createErr != nil {
			return fmt.Errorf("failed to create secret %q: %w", secretRef, createErr)
		}

		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to get secret %q: %w", secretRef, err)
	}

	// Update existing secret data and metadata
	existing.Data = secret.Data
	existing.Type = secret.Type
	existing.Labels = secret.Labels
	existing.Annotations = secret.Annotations

	updateErr := k8sClient.Update(ctx, existing)
	if updateErr != nil {
		return fmt.Errorf("failed to update secret %q: %w", secretRef, updateErr)
	}

	return nil
}

// buildDockerConfigJSON creates the .dockerconfigjson format for registry authentication.
func buildDockerConfigJSON(registry, username, password string) ([]byte, error) {
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

	config := map[string]any{
		"auths": map[string]any{
			registry: map[string]string{
				"username": username,
				"password": password,
				"auth":     auth,
			},
		},
	}

	data, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal docker config: %w", err)
	}

	return data, nil
}
