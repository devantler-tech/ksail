package fluxinstaller

import (
	"context"
	"crypto/subtle"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxclient "github.com/devantler-tech/ksail/v7/pkg/client/flux"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// managedByLabel and managedByKSail identify a registry Secret KSail owns.
// buildRegistrySecret stamps them on every Secret it writes, so their absence
// means the Secret was created by something else — an ExternalSecret, a
// platform bootstrap, or an operator — and KSail must not overwrite it.
const (
	managedByLabel = "app.kubernetes.io/managed-by"
	managedByKSail = "ksail"
)

// EnsureRegistryCredentials writes the resolved registry pull credential to the
// KSail-managed registry Secret. It is idempotent and independent of the
// structural configuration diff, so a caller can refresh a rotated credential
// without a configuration change and before any workload reconciliation that
// depends on the Secret.
//
// It is a no-op when the configured registry is not external or carries no
// credentials.
//
// kubeContext must be the same context the drift check read from. An empty
// context falls back to the kubeconfig's ambient current-context, which can be
// a different cluster entirely — so a rotation detected on one cluster would be
// written to another.
func EnsureRegistryCredentials(
	ctx context.Context,
	kubeconfig, kubeContext string,
	clusterCfg *v1alpha1.Cluster,
) error {
	if clusterCfg == nil {
		return errInvalidClusterConfig
	}

	restConfig, err := loadRESTConfig(kubeconfig, kubeContext)
	if err != nil {
		return err
	}

	return ensureExternalRegistrySecret(ctx, restConfig, clusterCfg)
}

// HasExternalRegistryCredentials reports whether the resolved configuration
// carries a registry credential KSail would write. It reads only the shape of
// the configuration, never the credential, so a caller can skip the cluster
// query below without handling secret material.
func HasExternalRegistryCredentials(clusterCfg *v1alpha1.Cluster) bool {
	if clusterCfg == nil {
		return false
	}

	localRegistry := clusterCfg.Spec.Cluster.LocalRegistry

	return localRegistry.IsExternal() && localRegistry.HasCredentials()
}

// RegistryCredentialDrifted reports whether the credential held in the
// KSail-managed registry Secret differs from the one the resolved configuration
// would write. Both values are compared inside this package, in constant time,
// and neither the credential nor anything derived from it is returned — the
// answer a caller needs is a single bit, so a single bit is all it gets.
//
// It reports false — never an error — when there is nothing KSail may safely
// compare against: the Secret does not exist, it holds no docker config, or it
// is not labelled as KSail-managed. That last case is the ownership boundary: a
// Secret supplied by an ExternalSecret must not be diffed into a change that
// would have KSail overwrite it.
//
// kubeContext selects the cluster to read. It must be the same context the
// subsequent credential write targets, or drift detected on one cluster would be
// repaired on another.
func RegistryCredentialDrifted(
	ctx context.Context,
	kubeconfig, kubeContext string,
	clusterCfg *v1alpha1.Cluster,
) (bool, error) {
	if !HasExternalRegistryCredentials(clusterCfg) {
		return false, nil
	}

	desired, err := buildRegistrySecret(clusterCfg)
	if err != nil {
		return false, fmt.Errorf("build desired registry secret: %w", err)
	}

	desiredConfig := desired.Data[corev1.DockerConfigJsonKey]
	if len(desiredConfig) == 0 {
		return false, nil
	}

	currentConfig, err := currentRegistryDockerConfig(ctx, kubeconfig, kubeContext)
	if err != nil {
		return false, err
	}

	return dockerConfigsDiffer(currentConfig, desiredConfig), nil
}

// dockerConfigsDiffer compares two docker configs in constant time.
//
// An empty side is never drift: an absent current config means the Secret does
// not exist or is not KSail-managed, and reporting drift there would have KSail
// overwrite a Secret it does not own. Comparing the credentials directly — rather
// than digests of them — keeps the secret material inside this package and leaves
// nothing guessable for a caller to leak.
func dockerConfigsDiffer(current, desired []byte) bool {
	if len(current) == 0 || len(desired) == 0 {
		return false
	}

	return subtle.ConstantTimeCompare(current, desired) == 0
}

// currentRegistryDockerConfig reads the docker config from the KSail-managed
// registry Secret, or returns nil when there is none KSail owns.
func currentRegistryDockerConfig(
	ctx context.Context,
	kubeconfig, kubeContext string,
) ([]byte, error) {
	restConfig, err := loadRESTConfig(kubeconfig, kubeContext)
	if err != nil {
		return nil, fmt.Errorf("build REST config: %w", err)
	}

	k8sClient, err := newCoreV1Client(restConfig)
	if err != nil {
		return nil, err
	}

	secret := &corev1.Secret{}
	key := client.ObjectKey{
		Name:      ExternalRegistrySecretName,
		Namespace: fluxclient.DefaultNamespace,
	}

	err = k8sClient.Get(ctx, key, secret)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("get registry secret: %w", err)
	}

	if secret.Labels[managedByLabel] != managedByKSail {
		return nil, nil
	}

	return secret.Data[corev1.DockerConfigJsonKey], nil
}
