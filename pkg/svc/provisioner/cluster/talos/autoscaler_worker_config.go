package talosprovisioner

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	clusterautoscalerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/clusterautoscaler"
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

	// autoscalerDeploymentSelector matches the cluster-autoscaler Deployment via the
	// standard Helm instance label, derived from the installer's ReleaseName so the
	// selector and the chart that stamps the label cannot drift apart.
	autoscalerDeploymentSelector = "app.kubernetes.io/instance=" + clusterautoscalerinstaller.ReleaseName

	// hetznerUserDataLimitBytes is Hetzner Cloud's hard ceiling on a server's
	// user_data field (32 KiB). In HCLOUD_CLUSTER_CONFIG mode the cluster-autoscaler
	// passes each pool's nodeConfigs[<pool>].cloudInit verbatim as the new server's
	// user_data, so that value must stay within this limit or Hetzner rejects every
	// scale-up with "invalid input in field 'user_data'".
	hetznerUserDataLimitBytes = 32768
)

// LabelAutoscaled is a Kubernetes node label stamped on every
// autoscaler-provisioned worker node and on no static baseline worker, giving
// downstream workloads a discriminator to key node affinity off of (e.g. a soft
// preference for baseline nodes so autoscaler nodes stay empty and scale down).
//
// It is applied via the worker config's machine.nodeLabels (kubelet
// --node-labels) so it lands on the real Node object. The Hetzner cluster
// autoscaler deliberately does NOT push its per-pool nodeConfigs[].labels to the
// kubelet — those only seed the in-memory scheduling-simulation template — so
// stamping the label in the worker cloud-init is the canonical mechanism (see
// kubernetes/autoscaler#8492, closed as working-as-intended).
const LabelAutoscaled = "ksail.io/autoscaled"

// GenerateAutoscalerWorkerConfig generates a stripped Talos worker config
// suitable for autoscaler-provisioned compute-only nodes. It sets
// machine.install.wipe to true, removes machine.disks (autoscaler nodes have
// no attached Hetzner Volumes), removes the Longhorn storage node label while
// preserving machine.kubelet.extraMounts for CSI consumer access, and stamps the
// LabelAutoscaled marker node label so workloads can tell autoscaler nodes apart
// from static baseline workers.
//
// poolLabels and poolTaints are the per-pool Kubernetes node labels and taints
// (both nil for a pool without any). They are baked into machine.nodeLabels and
// machine.nodeTaints so they land on the real Node object — the canonical
// mechanism, since the Hetzner cluster-autoscaler does NOT push its per-pool
// nodeConfigs[].labels/taints to the kubelet (those only seed the scale-from-zero
// template; see kubernetes/autoscaler#8492).
func GenerateAutoscalerWorkerConfig(
	workerConfig talosconfig.Provider,
	poolLabels map[string]string,
	poolTaints []corev1.Taint,
) ([]byte, error) {
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

		if cfg.MachineConfig.MachineNodeLabels == nil {
			cfg.MachineConfig.MachineNodeLabels = map[string]string{}
		}

		delete(cfg.MachineConfig.MachineNodeLabels, "node.longhorn.io/create-default-disk")
		cfg.MachineConfig.MachineNodeLabels[LabelAutoscaled] = "true"

		maps.Copy(cfg.MachineConfig.MachineNodeLabels, poolLabels)

		applyPoolTaints(cfg, poolTaints)

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

// applyPoolTaints writes the pool's taints into machine.nodeTaints. Talos encodes
// each taint as a map entry keyed by the taint key, with the value formatted as
// "<value>:<effect>" (or "<value>" when the effect is empty). Talos reconciles
// these onto the real Node via its node-labels controller, so they apply even
// under the kubelet NodeRestriction admission plugin.
func applyPoolTaints(cfg *v1alpha1.Config, poolTaints []corev1.Taint) {
	if len(poolTaints) == 0 {
		return
	}

	if cfg.MachineConfig.MachineNodeTaints == nil {
		cfg.MachineConfig.MachineNodeTaints = map[string]string{}
	}

	for _, taint := range poolTaints {
		value := taint.Value
		if taint.Effect != "" {
			value += ":" + string(taint.Effect)
		}

		cfg.MachineConfig.MachineNodeTaints[taint.Key] = value
	}
}

// AutoscalerPoolConfig is the per-pool data the cluster-autoscaler needs to
// provision and simulate nodes for one node pool. Name must match the pool name
// in the autoscaler's --nodes/autoscalingGroups entry (the key the autoscaler
// looks nodeConfigs up by). WorkerConfigYAML is the pool's Talos worker config
// (already including any pool labels/taints), which becomes the pool's cloud-init.
// Labels and Taints are attributed to the pool's scale-from-zero template node so
// the autoscaler scales the pool only for pods that select/tolerate them.
type AutoscalerPoolConfig struct {
	Name             string
	WorkerConfigYAML []byte
	Labels           map[string]string
	Taints           []corev1.Taint
}

// hcloudClusterConfig is the JSON document the Hetzner cluster-autoscaler reads
// from the HCLOUD_CLUSTER_CONFIG environment variable (base64-encoded). It carries
// the snapshot image per architecture and per-pool node configuration. The field
// json tags use the autoscaler's documented camelCase wire format.
type hcloudClusterConfig struct {
	ImagesForArch hcloudImageList             `json:"imagesForArch"`
	NodeConfigs   map[string]hcloudNodeConfig `json:"nodeConfigs"`
}

// hcloudImageList maps CPU architecture to a Hetzner image (name, ID, or label
// selector). KSail only builds amd64 Talos snapshots (ErrARM64SnapshotNotSupported),
// so only amd64 is populated; an arm64 pool would surface a clear missing-image
// error rather than silently booting an amd64 image.
type hcloudImageList struct {
	Arm64 string `json:"arm64,omitempty"`
	Amd64 string `json:"amd64"`
}

// hcloudNodeConfig is one pool's entry under nodeConfigs. cloudInit is used
// verbatim as the new server's user_data; labels and taints seed the
// scale-from-zero template node (the autoscaler does not push them to the kubelet).
type hcloudNodeConfig struct {
	CloudInit string            `json:"cloudInit"`
	Labels    map[string]string `json:"labels,omitempty"`
	Taints    []corev1.Taint    `json:"taints,omitempty"`
}

// buildClusterConfigSecretValue builds the base64-encoded HCLOUD_CLUSTER_CONFIG
// JSON from the snapshot image and per-pool configs. Each pool's cloud-init is
// compressed and size-checked (see compressWorkerConfigToUserData).
func buildClusterConfigSecretValue(
	snapshotImageID string,
	pools []AutoscalerPoolConfig,
) (string, error) {
	clusterConfig := hcloudClusterConfig{
		ImagesForArch: hcloudImageList{Amd64: snapshotImageID},
		NodeConfigs:   make(map[string]hcloudNodeConfig, len(pools)),
	}

	for _, pool := range pools {
		cloudInit, err := compressWorkerConfigToUserData(pool.WorkerConfigYAML)
		if err != nil {
			return "", fmt.Errorf("encode cloud-init for pool %q: %w", pool.Name, err)
		}

		clusterConfig.NodeConfigs[pool.Name] = hcloudNodeConfig{
			CloudInit: cloudInit,
			Labels:    pool.Labels,
			Taints:    pool.Taints,
		}
	}

	jsonBytes, err := json.Marshal(clusterConfig)
	if err != nil {
		return "", fmt.Errorf("marshal autoscaler cluster config: %w", err)
	}

	return base64.StdEncoding.EncodeToString(jsonBytes), nil
}

// snapshotImageIDFromSecret decodes the amd64 snapshot image ID currently stored
// in a cluster-autoscaler-config Secret's HCLOUD_CLUSTER_CONFIG value. It is used
// to detect a Talos OS bump (a new boot image) across an update: a changed image
// ID means existing autoscaler nodes booted from an older snapshot and can only
// adopt the new one by being replaced. It returns "" when the key is absent or
// the value cannot be decoded, so callers treat an unreadable baseline as "no
// detectable image change" rather than forcing a disruptive recycle.
func snapshotImageIDFromSecret(secret *corev1.Secret) string {
	raw := secret.Data[clusterautoscalerinstaller.AutoscalerConfigHcloudClusterConfigKey]
	if len(raw) == 0 {
		return ""
	}

	jsonBytes, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		return ""
	}

	var clusterConfig hcloudClusterConfig
	if json.Unmarshal(jsonBytes, &clusterConfig) != nil {
		return ""
	}

	return clusterConfig.ImagesForArch.Amd64
}

// currentAutoscalerSnapshotImageID returns the amd64 snapshot image ID currently
// recorded in the cluster-autoscaler-config Secret, or "" when the Secret is
// absent or unreadable. It is best-effort: an empty result simply means no boot
// image change can be detected, so the caller falls back to the diff-based gate.
func (p *Provisioner) currentAutoscalerSnapshotImageID(ctx context.Context) string {
	kubeclient, err := p.newSecretKubeclient("autoscaler snapshot probe")
	if err != nil {
		return ""
	}

	secret, err := kubeclient.CoreV1().
		Secrets(autoscalerConfigSecretNamespace).
		Get(ctx, autoscalerConfigSecretName, metav1.GetOptions{})
	if err != nil {
		return ""
	}

	return snapshotImageIDFromSecret(secret)
}

// compressWorkerConfigToUserData encodes a Talos worker machine config into the
// value the autoscaler stores verbatim as a Hetzner server's user_data.
//
// The result is base64(gzip(config)). Two constraints shape the encoding:
//
//   - Size: Hetzner caps user_data at 32 KiB, but a full Talos worker config is
//     ~40 KiB of YAML and overflows it (issue #5015). gzip shrinks it to a few KiB.
//   - Encoding: the value is embedded as a JSON string (in HCLOUD_CLUSTER_CONFIG)
//     and JSON-marshaled by hcloud-go as user_data, so it must be valid UTF-8 —
//     the base64 wrapper keeps it ASCII; raw gzip bytes would be corrupted.
//
// The Talos hcloud platform base64-decodes user_data (maybeBase64Decode) and then
// un-gzips it (gzip magic is auto-detected) before parsing — supported by every
// KSail-targeted Talos version (the hcloud base64 decode landed in Talos v1.8, 2024).
//
// Unlike the legacy HCLOUD_CLOUD_INIT (which the autoscaler base64-decoded once),
// HCLOUD_CLUSTER_CONFIG's cloudInit is used as-is, so only a single base64 layer
// is applied here.
func compressWorkerConfigToUserData(workerConfigYAML []byte) (string, error) {
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

	return userData, nil
}

// ApplyAutoscalerConfigSecret creates or updates the cluster-autoscaler-config
// Secret in kube-system. The Secret holds HCLOUD_CLUSTER_CONFIG: a base64-encoded
// JSON document carrying the Hetzner snapshot image and per-pool node configuration
// (cloud-init, labels, taints) the Kubernetes Cluster Autoscaler uses when
// provisioning and simulating new worker nodes (see buildClusterConfigSecretValue
// and compressWorkerConfigToUserData for the encoding rationale).
//
// It returns whether the Secret's data was created or changed. The autoscaler
// reads this key as an environment variable (valueFrom.secretKeyRef), which
// Kubernetes does not live-reload, so a true result means callers must restart
// the autoscaler Deployment for the new config to reach freshly provisioned nodes.
//
// The write is additive (MergeSecretData), so any legacy hcloud_image /
// hcloud_cloud_init keys from a pre-migration cluster are left in place — harmless
// once HCLOUD_CLUSTER_CONFIG is set (the autoscaler ignores the legacy vars) and
// keeping them avoids a missing-key crash during the brief restart-before-upgrade
// window on a migrating cluster update.
func ApplyAutoscalerConfigSecret(
	ctx context.Context,
	kubeclient kubernetes.Interface,
	snapshotImageID string,
	pools []AutoscalerPoolConfig,
) (bool, error) {
	clusterConfig, err := buildClusterConfigSecretValue(snapshotImageID, pools)
	if err != nil {
		return false, err
	}

	desiredData := map[string][]byte{
		clusterautoscalerinstaller.AutoscalerConfigHcloudClusterConfigKey: []byte(clusterConfig),
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
// updaters. It returns true only when it actually performed an update — if a
// concurrent writer already applied the desired data, it reports false so the
// caller does not roll the autoscaler for a Secret it did not change.
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
	updated := false

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

		updated = true

		return nil
	})
	if retryErr != nil {
		return false, fmt.Errorf("failed to update autoscaler config secret: %w", retryErr)
	}

	return updated, nil
}
