package talosprovisioner_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	clusterautoscalerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/clusterautoscaler"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	taloscontainer "github.com/siderolabs/talos/pkg/machinery/config/container"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// clusterConfigSecretKey is the Secret key under which the HCLOUD_CLUSTER_CONFIG
// JSON is stored.
const clusterConfigSecretKey = clusterautoscalerinstaller.AutoscalerConfigHcloudClusterConfigKey

// newTestWorkerProvider builds a minimal v1alpha1 Config wrapped in a Provider
// for use across autoscaler worker config tests.
func newTestWorkerProvider() *taloscontainer.Container {
	falseVal := false

	return taloscontainer.NewV1Alpha1(&v1alpha1.Config{
		ConfigVersion: "v1alpha1",
		MachineConfig: &v1alpha1.MachineConfig{
			MachineInstall: &v1alpha1.InstallConfig{
				InstallWipe: &falseVal,
			},
			MachineDisks: []*v1alpha1.MachineDisk{
				{DeviceName: "/dev/sdb"},
			},
			MachineNodeLabels: map[string]string{
				"node.longhorn.io/create-default-disk": "config",
				"keep-this-label":                      "value",
			},
			MachineKubelet: &v1alpha1.KubeletConfig{
				KubeletExtraMounts: []v1alpha1.ExtraMount{
					{Destination: "/var/lib/longhorn"},
				},
			},
		},
	})
}

// clusterConfigJSON mirrors the HCLOUD_CLUSTER_CONFIG document the autoscaler
// reads, for decoding in assertions.
type clusterConfigJSON struct {
	ImagesForArch struct {
		Arm64 string `json:"arm64"`
		Amd64 string `json:"amd64"`
	} `json:"imagesForArch"`
	NodeConfigs map[string]struct {
		CloudInit string            `json:"cloudInit"`
		Labels    map[string]string `json:"labels"`
		Taints    []corev1.Taint    `json:"taints"`
	} `json:"nodeConfigs"`
}

// decodeClusterConfig base64-decodes and JSON-parses the hcloud_cluster_config
// Secret value, the way the cluster-autoscaler does when reading the env var.
func decodeClusterConfig(t *testing.T, secretValue []byte) clusterConfigJSON {
	t.Helper()

	jsonBytes, err := base64.StdEncoding.DecodeString(string(secretValue))
	require.NoError(t, err, "outer base64 layer")

	var cfg clusterConfigJSON

	require.NoError(t, json.Unmarshal(jsonBytes, &cfg))

	return cfg
}

// decodePoolCloudInit reverses the per-pool cloud-init encoding the way the live
// chain does: the autoscaler uses nodeConfigs[pool].cloudInit verbatim as
// user_data, the Talos hcloud platform base64-decodes it (maybeBase64Decode) and
// un-gzips the result. The recovered bytes must equal the original worker config.
func decodePoolCloudInit(t *testing.T, cloudInit string) []byte {
	t.Helper()

	compressed, err := base64.StdEncoding.DecodeString(cloudInit)
	require.NoError(t, err, "Talos maybeBase64Decode layer")

	gzipReader, err := gzip.NewReader(bytes.NewReader(compressed))
	require.NoError(t, err, "gzip header")

	defer func() { require.NoError(t, gzipReader.Close()) }()

	decompressed, err := io.ReadAll(gzipReader)
	require.NoError(t, err)

	return decompressed
}

// singlePoolConfig builds a one-pool AutoscalerPoolConfig slice (pool "pool1")
// for tests that only exercise the encode/secret machinery and not per-pool
// labels/taints.
func singlePoolConfig(workerConfig []byte) []talosprovisioner.AutoscalerPoolConfig {
	return []talosprovisioner.AutoscalerPoolConfig{
		{Name: "pool1", WorkerConfigYAML: workerConfig},
	}
}

// isASCII reports whether every byte is in the 7-bit ASCII range. cloud-init must
// be ASCII so JSON marshaling does not corrupt it with U+FFFD.
func isASCII(b []byte) bool {
	for _, c := range b {
		if c > 0x7F {
			return false
		}
	}

	return true
}

// --- GenerateAutoscalerWorkerConfig ---

// generateAndParseAutoscalerConfig runs GenerateAutoscalerWorkerConfig on the
// given provider and returns the parsed v1alpha1 machine config, asserting the
// generate + parse round-trip succeeds. Shared by the stripping-logic, marker,
// and per-pool labels/taints tests so each stays focused on its own assertions.
func generateAndParseAutoscalerConfig(
	t *testing.T,
	provider *taloscontainer.Container,
	labels map[string]string,
	taints []corev1.Taint,
) *v1alpha1.Config {
	t.Helper()

	result, err := talosprovisioner.GenerateAutoscalerWorkerConfig(provider, labels, taints)
	require.NoError(t, err)
	require.NotEmpty(t, result)

	parsed, err := configloader.NewFromBytes(result)
	require.NoError(t, err)

	rawCfg := parsed.RawV1Alpha1()
	require.NotNil(t, rawCfg)
	require.NotNil(t, rawCfg.MachineConfig)

	return rawCfg
}

func TestGenerateAutoscalerWorkerConfig_StrippingLogic(t *testing.T) {
	t.Parallel()

	rawCfg := generateAndParseAutoscalerConfig(t, newTestWorkerProvider(), nil, nil)

	t.Run("sets install.wipe to true", func(t *testing.T) {
		t.Parallel()

		require.NotNil(t, rawCfg.MachineConfig.MachineInstall)
		require.NotNil(t, rawCfg.MachineConfig.MachineInstall.InstallWipe)
		assert.True(t, *rawCfg.MachineConfig.MachineInstall.InstallWipe)
	})

	t.Run("removes machine.disks", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, rawCfg.MachineConfig.MachineDisks) //nolint:staticcheck // deprecated field
	})

	t.Run("removes longhorn node label", func(t *testing.T) {
		t.Parallel()

		assert.NotContains(
			t,
			rawCfg.MachineConfig.MachineNodeLabels,
			"node.longhorn.io/create-default-disk",
		)
	})

	t.Run("preserves other node labels", func(t *testing.T) {
		t.Parallel()

		assert.Contains(t, rawCfg.MachineConfig.MachineNodeLabels, "keep-this-label")
	})

	t.Run("preserves kubelet extra mounts", func(t *testing.T) {
		t.Parallel()

		require.NotNil(t, rawCfg.MachineConfig.MachineKubelet)
		assert.NotEmpty(t, rawCfg.MachineConfig.MachineKubelet.KubeletExtraMounts)
		assert.Equal(
			t,
			"/var/lib/longhorn",
			rawCfg.MachineConfig.MachineKubelet.KubeletExtraMounts[0].Destination,
		)
	})
}

func TestGenerateAutoscalerWorkerConfig_StampsAutoscaledMarker(t *testing.T) {
	t.Parallel()

	rawCfg := generateAndParseAutoscalerConfig(t, newTestWorkerProvider(), nil, nil)

	// The marker discriminates autoscaler nodes from static baseline workers,
	// which never run through this generator and so never carry it.
	assert.Equal(
		t,
		"true",
		rawCfg.MachineConfig.MachineNodeLabels[talosprovisioner.LabelAutoscaled],
	)
}

func TestGenerateAutoscalerWorkerConfig_AppliesPoolLabelsAndTaints(t *testing.T) {
	t.Parallel()

	labels := map[string]string{"workload": "gpu"}
	taints := []corev1.Taint{
		{Key: "dedicated", Value: "gpu", Effect: corev1.TaintEffectNoSchedule},
		{Key: "spot", Effect: corev1.TaintEffectNoExecute}, // empty value
	}

	rawCfg := generateAndParseAutoscalerConfig(t, newTestWorkerProvider(), labels, taints)

	// Pool labels are baked into machine.nodeLabels alongside the marker, so they
	// land on the real Node object.
	assert.Equal(t, "gpu", rawCfg.MachineConfig.MachineNodeLabels["workload"])
	assert.Equal(
		t, "true", rawCfg.MachineConfig.MachineNodeLabels[talosprovisioner.LabelAutoscaled],
	)

	// Taints are encoded into machine.nodeTaints as "value:Effect" (or ":Effect"
	// when the value is empty).
	assert.Equal(t, "gpu:NoSchedule", rawCfg.MachineConfig.MachineNodeTaints["dedicated"])
	assert.Equal(t, ":NoExecute", rawCfg.MachineConfig.MachineNodeTaints["spot"])
}

func TestGenerateAutoscalerWorkerConfig_NilInput(t *testing.T) {
	t.Parallel()

	_, err := talosprovisioner.GenerateAutoscalerWorkerConfig(nil, nil, nil)
	require.ErrorIs(t, err, talosprovisioner.ErrNilWorkerConfig)
}

func TestGenerateAutoscalerWorkerConfig_InstallNilBeforePatch(t *testing.T) {
	t.Parallel()

	// Config with no MachineInstall set — the function should initialise it.
	provider := taloscontainer.NewV1Alpha1(&v1alpha1.Config{
		ConfigVersion: "v1alpha1",
		MachineConfig: &v1alpha1.MachineConfig{},
	})

	result, err := talosprovisioner.GenerateAutoscalerWorkerConfig(provider, nil, nil)
	require.NoError(t, err)

	parsed, err := configloader.NewFromBytes(result)
	require.NoError(t, err)

	rawCfg := parsed.RawV1Alpha1()
	require.NotNil(t, rawCfg)
	require.NotNil(t, rawCfg.MachineConfig.MachineInstall)
	require.NotNil(t, rawCfg.MachineConfig.MachineInstall.InstallWipe)
	assert.True(t, *rawCfg.MachineConfig.MachineInstall.InstallWipe)
}

func TestGenerateAutoscalerWorkerConfig_NilMachineConfig(t *testing.T) {
	t.Parallel()

	// Config with nil MachineConfig — the function should initialise it and not panic.
	provider := taloscontainer.NewV1Alpha1(&v1alpha1.Config{
		ConfigVersion: "v1alpha1",
	})

	result, err := talosprovisioner.GenerateAutoscalerWorkerConfig(provider, nil, nil)
	require.NoError(t, err)

	parsed, err := configloader.NewFromBytes(result)
	require.NoError(t, err)

	rawCfg := parsed.RawV1Alpha1()
	require.NotNil(t, rawCfg)
	require.NotNil(t, rawCfg.MachineConfig)
	require.NotNil(t, rawCfg.MachineConfig.MachineInstall)
	require.NotNil(t, rawCfg.MachineConfig.MachineInstall.InstallWipe)
	assert.True(t, *rawCfg.MachineConfig.MachineInstall.InstallWipe)

	// The marker label must be stamped even when the source bundle carried no
	// node labels at all (nil MachineNodeLabels), exercising the nil-map guard.
	assert.Equal(
		t,
		"true",
		rawCfg.MachineConfig.MachineNodeLabels[talosprovisioner.LabelAutoscaled],
	)
}

// --- ApplyAutoscalerConfigSecret ---

func TestApplyAutoscalerConfigSecret_CreatesNewSecret(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()
	snapshotID := "123456789"
	workerConfig := []byte("machine:\n  type: worker\n")

	_, err := talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(),
		clientset,
		snapshotID,
		singlePoolConfig(workerConfig),
	)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().Secrets("kube-system").Get(
		context.Background(),
		"cluster-autoscaler-config",
		metav1.GetOptions{},
	)
	require.NoError(t, err)

	cfg := decodeClusterConfig(t, secret.Data[clusterConfigSecretKey])
	assert.Equal(t, snapshotID, cfg.ImagesForArch.Amd64)
	require.Contains(t, cfg.NodeConfigs, "pool1")
	assert.Equal(t, workerConfig, decodePoolCloudInit(t, cfg.NodeConfigs["pool1"].CloudInit))
}

func TestApplyAutoscalerConfigSecret_PerPoolLabelsTaints(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()
	poolA := []byte("machine:\n  type: worker\n  # pool a\n")
	poolB := []byte("machine:\n  type: worker\n  # pool b\n")
	pools := []talosprovisioner.AutoscalerPoolConfig{
		{
			Name:             "a",
			WorkerConfigYAML: poolA,
			Labels: map[string]string{
				"workload":                       "a",
				talosprovisioner.LabelAutoscaled: "true",
			},
		},
		{
			Name:             "b",
			WorkerConfigYAML: poolB,
			Labels:           map[string]string{talosprovisioner.LabelAutoscaled: "true"},
			Taints: []corev1.Taint{
				{Key: "dedicated", Value: "b", Effect: corev1.TaintEffectNoSchedule},
			},
		},
	}

	_, err := talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(), clientset, "55", pools,
	)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().Secrets("kube-system").Get(
		context.Background(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)

	cfg := decodeClusterConfig(t, secret.Data[clusterConfigSecretKey])

	// Each pool gets its own cloud-init (its own worker config round-trips).
	assert.Equal(t, poolA, decodePoolCloudInit(t, cfg.NodeConfigs["a"].CloudInit))
	assert.Equal(t, poolB, decodePoolCloudInit(t, cfg.NodeConfigs["b"].CloudInit))

	// Per-pool labels/taints seed the scale-from-zero template.
	assert.Equal(t, "a", cfg.NodeConfigs["a"].Labels["workload"])
	assert.Empty(t, cfg.NodeConfigs["a"].Taints)
	require.Len(t, cfg.NodeConfigs["b"].Taints, 1)
	assert.Equal(t, "dedicated", cfg.NodeConfigs["b"].Taints[0].Key)
	assert.Equal(t, corev1.TaintEffectNoSchedule, cfg.NodeConfigs["b"].Taints[0].Effect)
}

func TestApplyAutoscalerConfigSecret_ReturnsChangedFlag(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()
	pools := singlePoolConfig([]byte("machine:\n  type: worker\n"))

	// First apply creates the secret => changed.
	changed, err := talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(), clientset, "123456789", pools,
	)
	require.NoError(t, err)
	assert.True(t, changed, "creating the secret must report a change")

	// Re-applying identical inputs is a no-op => not changed. This is what gates
	// the autoscaler Deployment restart, so an unrelated `cluster update` does not
	// needlessly bounce the autoscaler.
	changed, err = talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(), clientset, "123456789", pools,
	)
	require.NoError(t, err)
	assert.False(t, changed, "re-applying identical config must report no change")
}

func TestApplyAutoscalerConfigSecret_PreservesExtraKeysOnUpdate(t *testing.T) {
	t.Parallel()

	// A pre-migration secret carries the legacy keys plus an unrelated extra key.
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-autoscaler-config",
			Namespace: "kube-system",
		},
		Data: map[string][]byte{
			"hcloud_image":      []byte("old-image-id"),
			"hcloud_cloud_init": []byte("old-config"),
			"extra_key":         []byte("extra-value"),
		},
	}
	clientset := fake.NewClientset(existing)

	snapshotID := "111111111"
	workerConfig := []byte("machine:\n  type: worker\n")

	_, err := talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(),
		clientset,
		snapshotID,
		singlePoolConfig(workerConfig),
	)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().Secrets("kube-system").Get(
		context.Background(),
		"cluster-autoscaler-config",
		metav1.GetOptions{},
	)
	require.NoError(t, err)

	// The new cluster-config key is written.
	cfg := decodeClusterConfig(t, secret.Data[clusterConfigSecretKey])
	assert.Equal(t, snapshotID, cfg.ImagesForArch.Amd64)
	assert.Equal(t, workerConfig, decodePoolCloudInit(t, cfg.NodeConfigs["pool1"].CloudInit))

	// Extra key must be preserved (merge, not replace) — keeping the legacy keys
	// around is what avoids a missing-key crash during the migration window.
	assert.Equal(t, []byte("extra-value"), secret.Data["extra_key"])
}

func TestApplyAutoscalerConfigSecret_UpdatesExistingSecret(t *testing.T) {
	t.Parallel()

	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-autoscaler-config",
			Namespace: "kube-system",
		},
		Data: map[string][]byte{
			clusterConfigSecretKey: []byte("old-cluster-config"),
		},
	}
	clientset := fake.NewClientset(existing)

	snapshotID := "987654321"
	workerConfig := []byte("machine:\n  type: worker\n  install:\n    wipe: true\n")

	_, err := talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(),
		clientset,
		snapshotID,
		singlePoolConfig(workerConfig),
	)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().Secrets("kube-system").Get(
		context.Background(),
		"cluster-autoscaler-config",
		metav1.GetOptions{},
	)
	require.NoError(t, err)

	cfg := decodeClusterConfig(t, secret.Data[clusterConfigSecretKey])
	assert.Equal(t, snapshotID, cfg.ImagesForArch.Amd64)
	assert.Equal(t, workerConfig, decodePoolCloudInit(t, cfg.NodeConfigs["pool1"].CloudInit))
}

// newConflictClientset returns a fake clientset pre-seeded with an existing
// cluster-autoscaler-config Secret and reactors that simulate concurrent
// creation: the first Get returns NotFound (entering the create path) and
// Create returns AlreadyExists (simulating a concurrent writer).
func newConflictClientset(preExisting *corev1.Secret) *fake.Clientset {
	clientset := fake.NewClientset(preExisting)

	getCallCount := 0

	clientset.PrependReactor(
		"get",
		"secrets",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			getCallCount++
			if getCallCount == 1 {
				return true, nil, apierrors.NewNotFound(
					schema.GroupResource{Group: "", Resource: "secrets"},
					"cluster-autoscaler-config",
				)
			}

			return false, nil, nil
		},
	)

	clientset.PrependReactor(
		"create",
		"secrets",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewAlreadyExists(
				schema.GroupResource{Group: "", Resource: "secrets"},
				"cluster-autoscaler-config",
			)
		},
	)

	return clientset
}

func TestApplyAutoscalerConfigSecret_CreateConflictFallsBackToUpdate(t *testing.T) {
	t.Parallel()

	preExisting := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "cluster-autoscaler-config",
			Namespace:       "kube-system",
			ResourceVersion: "1",
		},
		Data: map[string][]byte{
			clusterConfigSecretKey: []byte("old-cluster-config"),
		},
	}
	clientset := newConflictClientset(preExisting)

	newConfig := []byte("machine:\n  type: worker\n  # new\n")

	_, err := talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(), clientset, "new-image", singlePoolConfig(newConfig),
	)
	require.NoError(t, err)

	// Verify the Secret was actually updated with the new values (not a no-op).
	updated, err := clientset.CoreV1().Secrets("kube-system").Get(
		context.Background(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)

	cfg := decodeClusterConfig(t, updated.Data[clusterConfigSecretKey])
	assert.Equal(t, "new-image", cfg.ImagesForArch.Amd64)
	assert.Equal(t, newConfig, decodePoolCloudInit(t, cfg.NodeConfigs["pool1"].CloudInit))
}

// largeWorkerConfigYAML builds a worker-config-shaped YAML blob larger than
// Hetzner's 32 KiB user_data limit — the situation from issue #5015 where the
// raw config overflowed. It mixes a high-entropy PKI chunk (incompressible) with
// repetitive structure (compressible), mirroring a real Talos worker config.
func largeWorkerConfigYAML(t *testing.T) []byte {
	t.Helper()

	pki := make([]byte, 3072)
	_, err := rand.Read(pki)
	require.NoError(t, err)

	var builder strings.Builder

	builder.WriteString("version: v1alpha1\nmachine:\n  type: worker\n  ca:\n    crt: ")
	builder.WriteString(base64.StdEncoding.EncodeToString(pki))
	builder.WriteString("\n  registries:\n    mirrors:\n")

	for i := range 400 {
		fmt.Fprintf(
			&builder,
			"      registry-%d.example.com:\n        endpoints:\n          - https://mirror.example.com/v2\n",
			i,
		)
	}

	return []byte(builder.String())
}

// A realistic ~40 KiB worker config (the size that overflowed Hetzner's raw
// user_data limit in issue #5015) must compress to a cloud-init payload that is
// both under 32 KiB and ASCII, and must round-trip back to the original config.
func TestApplyAutoscalerConfigSecret_CompressesLargeConfigUnderLimit(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()
	largeConfig := largeWorkerConfigYAML(t)

	require.Greater(t, len(largeConfig), 32768,
		"test fixture must exceed Hetzner's raw user_data limit to be representative")

	_, err := talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(), clientset, "123456789", singlePoolConfig(largeConfig),
	)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().Secrets("kube-system").Get(
		context.Background(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)

	cfg := decodeClusterConfig(t, secret.Data[clusterConfigSecretKey])
	cloudInit := cfg.NodeConfigs["pool1"].CloudInit

	assert.LessOrEqual(t, len(cloudInit), 32768,
		"gzip must bring cloud-init under Hetzner's 32 KiB user_data limit")
	assert.True(t, isASCII([]byte(cloudInit)),
		"cloud-init must be ASCII so JSON marshaling does not corrupt it")
	assert.Equal(t, largeConfig, decodePoolCloudInit(t, cloudInit),
		"config must round-trip through the autoscaler + Talos decode chain")
}

// When even the gzip-compressed config exceeds Hetzner's 32 KiB user_data limit,
// ApplyAutoscalerConfigSecret must fail fast (at cluster create/update) rather
// than write a secret that would silently break the next scale-up.
func TestApplyAutoscalerConfigSecret_RejectsOversizedConfig(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()

	// Incompressible random bytes large enough that base64(gzip(data)) still
	// exceeds 32 KiB, exercising the fail-fast guard.
	incompressible := make([]byte, 40000)
	_, err := rand.Read(incompressible)
	require.NoError(t, err)

	_, err = talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(), clientset, "123456789", singlePoolConfig(incompressible),
	)
	require.ErrorIs(t, err, talosprovisioner.ErrAutoscalerUserDataTooLarge)

	// The guard must fire before any Secret is written.
	_, getErr := clientset.CoreV1().Secrets("kube-system").Get(
		context.Background(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	assert.True(t, apierrors.IsNotFound(getErr))
}
