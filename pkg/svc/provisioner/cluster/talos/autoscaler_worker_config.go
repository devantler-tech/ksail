package talosprovisioner

import (
	"context"
	"encoding/base64"
	"fmt"

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

// ApplyAutoscalerConfigSecret creates or updates the cluster-autoscaler-config
// Secret in kube-system. The Secret holds the Hetzner snapshot image ID and a
// base64-encoded Talos worker config that the node-autoscaler uses as
// cloud-init user-data when provisioning new worker nodes.
func ApplyAutoscalerConfigSecret(
	ctx context.Context,
	kubeclient kubernetes.Interface,
	snapshotImageID string,
	workerConfigYAML []byte,
) error {
	encodedConfig := base64.StdEncoding.EncodeToString(workerConfigYAML)

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

	_, err := secretsClient.Get(ctx, autoscalerConfigSecretName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get autoscaler config secret: %w", err)
		}

		_, createErr := secretsClient.Create(ctx, secret, metav1.CreateOptions{})
		if createErr != nil {
			return fmt.Errorf("create autoscaler config secret: %w", createErr)
		}

		return nil
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest, getErr := secretsClient.Get(ctx, autoscalerConfigSecretName, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("get autoscaler config secret for update: %w", getErr)
		}

		latest.Data = desiredData
		latest.StringData = nil

		_, updateErr := secretsClient.Update(ctx, latest, metav1.UpdateOptions{})
		if updateErr != nil {
			return fmt.Errorf("update autoscaler config secret: %w", updateErr)
		}

		return nil
	})
	if retryErr != nil {
		return fmt.Errorf("failed to update autoscaler config secret: %w", retryErr)
	}

	return nil
}
