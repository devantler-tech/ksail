package talosprovisioner

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const (
	autoscalerConfigSecretName      = "cluster-autoscaler-config"
	autoscalerConfigSecretNamespace = "kube-system"

	// autoscalerDeploymentSelector matches the cluster-autoscaler Deployment via
	// the standard Helm instance label. It must stay in sync with the chart
	// ReleaseName ("cluster-autoscaler") in pkg/svc/installer/clusterautoscaler.
	autoscalerDeploymentSelector = "app.kubernetes.io/instance=cluster-autoscaler"

	// hetznerUserDataLimitBytes is Hetzner Cloud's hard ceiling on a server's
	// user_data field (32 KiB). The cluster-autoscaler base64-decodes the
	// HCLOUD_CLOUD_INIT secret value exactly once and passes the result verbatim
	// as user_data, so that post-decode payload must stay within this limit or
	// Hetzner rejects every scale-up with "invalid input in field 'user_data'".
	hetznerUserDataLimitBytes = 32768
)

// GenerateAutoscalerWorkerConfig generates a stripped Talos worker config
// suitable for autoscaler-provisioned compute-only nodes. It sets
// machine.install.wipe to true, removes machine.disks (autoscaler nodes have
// no attached Hetzner Volumes), and removes the Longhorn storage node label
// while preserving machine.kubelet.extraMounts for CSI consumer access.
func GenerateAutoscalerWorkerConfig(workerConfig talosconfig.Provider) ([]byte, error) {
	if workerConfig == nil {
		return nil, ErrNilWorkerConfig
	}

	patched, err := workerConfig.PatchV1Alpha1(func(cfg *v1alpha1.Config) error {
		if cfg.MachineConfig == nil {
			cfg.MachineConfig = &v1alpha1.MachineConfig{}
		}

		if cfg.MachineConfig.MachineInstall == nil {
			cfg.MachineConfig.MachineInstall = &v1alpha1.InstallConfig{}
		}

		wipe := true
		cfg.MachineConfig.MachineInstall.InstallWipe = &wipe

		cfg.MachineConfig.MachineDisks = nil //nolint:staticcheck // deprecated; v1alpha1.Config has no replacement field

		delete(cfg.MachineConfig.MachineNodeLabels, "node.longhorn.io/create-default-disk")

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("patch autoscaler worker config: %w", err)
	}

	cfgBytes, err := patched.Bytes()
	if err != nil {
		return nil, fmt.Errorf("marshal autoscaler worker config: %w", err)
	}

	return cfgBytes, nil
}

// encodeAutoscalerCloudInit encodes a Talos worker machine config for delivery
// to autoscaler-provisioned Hetzner nodes and returns the value to store under
// the HCLOUD_CLOUD_INIT secret key.
//
// The cluster-autoscaler base64-decodes HCLOUD_CLOUD_INIT exactly once and
// assigns the result verbatim to the new server's user_data. Two constraints
// shape the encoding:
//
//   - Size: Hetzner caps user_data at 32 KiB, but a full Talos worker config is
//     ~40 KiB of YAML and overflows it (issue #5015).
//   - Encoding: hcloud-go JSON-marshals user_data as a string, so it must be
//     valid UTF-8 — raw gzip bytes would be mangled by JSON's invalid-byte
//     replacement.
//
// Both hold when user_data = base64(gzip(config)): gzip shrinks the config to a
// few KiB and the base64 wrapper keeps it ASCII. The Talos hcloud platform
// base64-decodes user_data (maybeBase64Decode) and then un-gzips it (gzip magic
// is auto-detected) before parsing — supported by every KSail-targeted Talos
// version (the hcloud base64 decode landed in Talos v1.8, 2024).
//
// The returned value is base64(user_data): the autoscaler strips this outer
// base64 layer, leaving base64(gzip(config)) as the user_data Hetzner stores.
func encodeAutoscalerCloudInit(workerConfigYAML []byte) (string, error) {
	var compressed bytes.Buffer

	gzipWriter := gzip.NewWriter(&compressed)

	_, err := gzipWriter.Write(workerConfigYAML)
	if err != nil {
		return "", fmt.Errorf("gzip autoscaler worker config: %w", err)
	}

	err = gzipWriter.Close()
	if err != nil {
		return "", fmt.Errorf("finalize gzip autoscaler worker config: %w", err)
	}

	// userData is exactly what Hetzner stores and what its 32 KiB limit governs.
	userData := base64.StdEncoding.EncodeToString(compressed.Bytes())
	if len(userData) > hetznerUserDataLimitBytes {
		return "", fmt.Errorf(
			"%w: compressed user_data is %d bytes (limit %d)",
			ErrAutoscalerUserDataTooLarge,
			len(userData),
			hetznerUserDataLimitBytes,
		)
	}

	return base64.StdEncoding.EncodeToString([]byte(userData)), nil
}

// ApplyAutoscalerConfigSecret creates or updates the cluster-autoscaler-config
// Secret in kube-system. The Secret holds the Hetzner snapshot image ID and the
// gzip-compressed, base64-encoded Talos worker config that Kubernetes Cluster
// Autoscaler uses as cloud-init user-data when provisioning new worker nodes
// (see encodeAutoscalerCloudInit for the encoding rationale).
//
// It returns whether the Secret's data was created or changed. The autoscaler
// reads these keys as environment variables (valueFrom.secretKeyRef), which
// Kubernetes does not live-reload, so a true result means callers must restart
// the autoscaler Deployment for the new config to reach freshly provisioned nodes.
func ApplyAutoscalerConfigSecret(
	ctx context.Context,
	kubeclient kubernetes.Interface,
	snapshotImageID string,
	workerConfigYAML []byte,
) (bool, error) {
	encodedConfig, err := encodeAutoscalerCloudInit(workerConfigYAML)
	if err != nil {
		return false, err
	}

	desiredData := map[string][]byte{
		"hcloud_image":      []byte(snapshotImageID),
		"hcloud_cloud_init": []byte(encodedConfig),
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      autoscalerConfigSecretName,
			Namespace: autoscalerConfigSecretNamespace,
		},
		Data: desiredData,
	}

	secretsClient := kubeclient.CoreV1().Secrets(autoscalerConfigSecretNamespace)

	existing, err := secretsClient.Get(ctx, autoscalerConfigSecretName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("get autoscaler config secret: %w", err)
		}

		return createOrUpdateAutoscalerSecretOnConflict(ctx, kubeclient, secret)
	}

	return updateAutoscalerSecretIfNeeded(ctx, kubeclient, existing, desiredData)
}

// createOrUpdateAutoscalerSecretOnConflict creates the Secret. If a concurrent
// caller already created it between the outer Get and this Create, it falls
// back to a merge-update to stay idempotent. It returns whether the Secret's data
// was created or changed.
func createOrUpdateAutoscalerSecretOnConflict(
	ctx context.Context,
	client kubernetes.Interface,
	secret *corev1.Secret,
) (bool, error) {
	secretsClient := client.CoreV1().Secrets(autoscalerConfigSecretNamespace)

	_, err := secretsClient.Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return false, fmt.Errorf("create autoscaler config secret: %w", err)
		}

		existing, getErr := secretsClient.Get(ctx, autoscalerConfigSecretName, metav1.GetOptions{})
		if getErr != nil {
			return false, fmt.Errorf("get autoscaler config secret after conflict: %w", getErr)
		}

		return updateAutoscalerSecretIfNeeded(ctx, client, existing, secret.Data)
	}

	return true, nil
}

// updateAutoscalerSecretIfNeeded merges the desired keys into the existing
// Secret. It skips the update when all desired keys already match to avoid
// unnecessary API calls. RetryOnConflict handles 409 responses from concurrent
// updaters. It returns whether the Secret's data was changed.
func updateAutoscalerSecretIfNeeded(
	ctx context.Context,
	client kubernetes.Interface,
	existing *corev1.Secret,
	desiredData map[string][]byte,
) (bool, error) {
	if !k8s.MergeSecretData(existing, desiredData) {
		return false, nil
	}

	secretsClient := client.CoreV1().Secrets(autoscalerConfigSecretNamespace)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest, getErr := secretsClient.Get(ctx, autoscalerConfigSecretName, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("get autoscaler config secret for update: %w", getErr)
		}

		if !k8s.MergeSecretData(latest, desiredData) {
			return nil
		}

		_, updateErr := secretsClient.Update(ctx, latest, metav1.UpdateOptions{})
		if updateErr != nil {
			return fmt.Errorf("update autoscaler config secret: %w", updateErr)
		}

		return nil
	})
	if retryErr != nil {
		return false, fmt.Errorf("failed to update autoscaler config secret: %w", retryErr)
	}

	return true, nil
}
