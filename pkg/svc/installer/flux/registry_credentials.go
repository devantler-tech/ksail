package fluxinstaller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
func EnsureRegistryCredentials(
	ctx context.Context,
	kubeconfig string,
	clusterCfg *v1alpha1.Cluster,
) error {
	if clusterCfg == nil {
		return errInvalidClusterConfig
	}

	if ctx == nil {
		ctx = context.Background()
	}

	restConfig, err := loadRESTConfig(kubeconfig, "")
	if err != nil {
		return err
	}

	return ensureExternalRegistrySecret(ctx, restConfig, clusterCfg)
}

// DesiredRegistryCredentialDigest returns a digest of the docker config KSail
// would write for the resolved configuration, or an empty string when the
// registry is not external or carries no credentials.
//
// The digest never leaves the process: it exists so a rotation can be detected
// without the caller ever handling — or rendering — the credential itself.
func DesiredRegistryCredentialDigest(clusterCfg *v1alpha1.Cluster) (string, error) {
	if clusterCfg == nil {
		return "", nil
	}

	localRegistry := clusterCfg.Spec.Cluster.LocalRegistry
	if !localRegistry.IsExternal() || !localRegistry.HasCredentials() {
		return "", nil
	}

	secret, err := buildRegistrySecret(clusterCfg)
	if err != nil {
		return "", fmt.Errorf("build desired registry secret: %w", err)
	}

	return digestDockerConfig(secret.Data[corev1.DockerConfigJsonKey]), nil
}

// CurrentRegistryCredentialDigest reads the registry Secret from the cluster and
// returns a digest of its docker config.
//
// It returns an empty digest — never an error — when there is nothing KSail may
// safely compare against: the Secret does not exist, it holds no docker config,
// or it is not labelled as KSail-managed. That last case is the ownership
// boundary: a Secret supplied by an ExternalSecret must not be diffed into a
// change that would have KSail overwrite it.
func CurrentRegistryCredentialDigest(
	ctx context.Context,
	kubeconfig, kubeContext string,
) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	restConfig, err := loadRESTConfig(kubeconfig, kubeContext)
	if err != nil {
		return "", fmt.Errorf("build REST config: %w", err)
	}

	k8sClient, err := newCoreV1Client(restConfig)
	if err != nil {
		return "", err
	}

	secret := &corev1.Secret{}
	key := client.ObjectKey{
		Name:      ExternalRegistrySecretName,
		Namespace: fluxclient.DefaultNamespace,
	}

	err = k8sClient.Get(ctx, key, secret)
	if apierrors.IsNotFound(err) {
		return "", nil
	}

	if err != nil {
		return "", fmt.Errorf("get registry secret: %w", err)
	}

	if secret.Labels[managedByLabel] != managedByKSail {
		return "", nil
	}

	dockerConfig := secret.Data[corev1.DockerConfigJsonKey]
	if len(dockerConfig) == 0 {
		return "", nil
	}

	return digestDockerConfig(dockerConfig), nil
}

// digestDockerConfig reduces a docker config to a comparable digest. An empty
// input yields an empty digest so "absent" never collides with a real value —
// a digest of the empty string is a fixed constant, and returning it would make
// a missing credential compare equal to another missing credential and, worse,
// unequal to every real one.
func digestDockerConfig(dockerConfig []byte) string {
	if len(dockerConfig) == 0 {
		return ""
	}

	sum := sha256.Sum256(dockerConfig)

	return hex.EncodeToString(sum[:])
}
