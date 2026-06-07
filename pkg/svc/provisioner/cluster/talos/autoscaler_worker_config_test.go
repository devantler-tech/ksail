package talosprovisioner_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"testing"

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

// decodeAutoscalerCloudInit reverses encodeAutoscalerCloudInit the way the live
// chain does: the cluster-autoscaler strips the outer base64 layer to form
// user_data, the Talos hcloud platform base64-decodes user_data again
// (maybeBase64Decode), and the platform config loader un-gzips the result before
// parsing. The recovered bytes must equal the original worker config.
func decodeAutoscalerCloudInit(t *testing.T, secretValue []byte) []byte {
	t.Helper()

	userData, err := base64.StdEncoding.DecodeString(string(secretValue))
	require.NoError(t, err, "autoscaler base64 layer")

	compressed, err := base64.StdEncoding.DecodeString(string(userData))
	require.NoError(t, err, "Talos maybeBase64Decode layer")

	gzipReader, err := gzip.NewReader(bytes.NewReader(compressed))
	require.NoError(t, err, "gzip header")

	defer func() { require.NoError(t, gzipReader.Close()) }()

	decompressed, err := io.ReadAll(gzipReader)
	require.NoError(t, err)

	return decompressed
}

// autoscalerUserData returns the inner layer of the secret value: the exact
// bytes Hetzner stores as user_data (base64(gzip(config))), against which the
// 32 KiB limit is enforced.
func autoscalerUserData(t *testing.T, secretValue []byte) []byte {
	t.Helper()

	userData, err := base64.StdEncoding.DecodeString(string(secretValue))
	require.NoError(t, err)

	return userData
}

// isASCII reports whether every byte is in the 7-bit ASCII range. user_data must
// be ASCII so hcloud-go's JSON marshaling does not corrupt it with U+FFFD.
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
// generate + parse round-trip succeeds. Shared by the stripping-logic and marker
// tests so each stays focused on its own assertions.
func generateAndParseAutoscalerConfig(
	t *testing.T,
	provider *taloscontainer.Container,
) *v1alpha1.Config {
	t.Helper()

	result, err := talosprovisioner.GenerateAutoscalerWorkerConfig(provider)
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

	rawCfg := generateAndParseAutoscalerConfig(t, newTestWorkerProvider())

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

	rawCfg := generateAndParseAutoscalerConfig(t, newTestWorkerProvider())

	// The marker discriminates autoscaler nodes from static baseline workers,
	// which never run through this generator and so never carry it.
	assert.Equal(
		t,
		"true",
		rawCfg.MachineConfig.MachineNodeLabels[talosprovisioner.LabelAutoscaled],
	)
}

func TestGenerateAutoscalerWorkerConfig_NilInput(t *testing.T) {
	t.Parallel()

	_, err := talosprovisioner.GenerateAutoscalerWorkerConfig(nil)
	require.ErrorIs(t, err, talosprovisioner.ErrNilWorkerConfig)
}

func TestGenerateAutoscalerWorkerConfig_InstallNilBeforePatch(t *testing.T) {
	t.Parallel()

	// Config with no MachineInstall set — the function should initialise it.
	provider := taloscontainer.NewV1Alpha1(&v1alpha1.Config{
		ConfigVersion: "v1alpha1",
		MachineConfig: &v1alpha1.MachineConfig{},
	})

	result, err := talosprovisioner.GenerateAutoscalerWorkerConfig(provider)
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

	result, err := talosprovisioner.GenerateAutoscalerWorkerConfig(provider)
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

func TestApplyAutoscalerConfigSecret_CreatesNewSecret(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()
	snapshotID := "123456789"
	workerConfig := []byte("machine:\n  type: worker\n")

	_, err := talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(),
		clientset,
		snapshotID,
		workerConfig,
	)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().Secrets("kube-system").Get(
		context.Background(),
		"cluster-autoscaler-config",
		metav1.GetOptions{},
	)
	require.NoError(t, err)

	assert.Equal(t, []byte(snapshotID), secret.Data["hcloud_image"])

	assert.Equal(t, workerConfig, decodeAutoscalerCloudInit(t, secret.Data["hcloud_cloud_init"]))
}

func TestApplyAutoscalerConfigSecret_ReturnsChangedFlag(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()
	workerConfig := []byte("machine:\n  type: worker\n")

	// First apply creates the secret => changed.
	changed, err := talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(), clientset, "123456789", workerConfig,
	)
	require.NoError(t, err)
	assert.True(t, changed, "creating the secret must report a change")

	// Re-applying identical inputs is a no-op => not changed. This is what gates
	// the autoscaler Deployment restart, so an unrelated `cluster update` does not
	// needlessly bounce the autoscaler.
	changed, err = talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(), clientset, "123456789", workerConfig,
	)
	require.NoError(t, err)
	assert.False(t, changed, "re-applying identical config must report no change")
}

func TestApplyAutoscalerConfigSecret_PreservesExtraKeysOnUpdate(t *testing.T) {
	t.Parallel()

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
		workerConfig,
	)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().Secrets("kube-system").Get(
		context.Background(),
		"cluster-autoscaler-config",
		metav1.GetOptions{},
	)
	require.NoError(t, err)

	// Desired keys must be updated.
	assert.Equal(t, []byte(snapshotID), secret.Data["hcloud_image"])

	assert.Equal(t, workerConfig, decodeAutoscalerCloudInit(t, secret.Data["hcloud_cloud_init"]))

	// Extra key must be preserved (merge, not replace).
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
			"hcloud_image":      []byte("old-image-id"),
			"hcloud_cloud_init": []byte("old-config"),
		},
	}
	clientset := fake.NewClientset(existing)

	snapshotID := "987654321"
	workerConfig := []byte("machine:\n  type: worker\n  install:\n    wipe: true\n")

	_, err := talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(),
		clientset,
		snapshotID,
		workerConfig,
	)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().Secrets("kube-system").Get(
		context.Background(),
		"cluster-autoscaler-config",
		metav1.GetOptions{},
	)
	require.NoError(t, err)

	assert.Equal(t, []byte(snapshotID), secret.Data["hcloud_image"])

	assert.Equal(t, workerConfig, decodeAutoscalerCloudInit(t, secret.Data["hcloud_cloud_init"]))
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
			"hcloud_image":      []byte("old-image"),
			"hcloud_cloud_init": []byte("old-config"),
		},
	}
	clientset := newConflictClientset(preExisting)

	_, err := talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(), clientset, "new-image", []byte("new-config"),
	)
	require.NoError(t, err)

	// Verify the Secret was actually updated with the new values (not a no-op).
	updated, err := clientset.CoreV1().Secrets("kube-system").Get(
		context.Background(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)

	assert.Equal(t, []byte("new-image"), updated.Data["hcloud_image"])

	assert.Equal(
		t,
		[]byte("new-config"),
		decodeAutoscalerCloudInit(t, updated.Data["hcloud_cloud_init"]),
	)
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
// user_data limit in issue #5015) must compress to a user_data payload that is
// both under 32 KiB and ASCII, and must round-trip back to the original config.
func TestApplyAutoscalerConfigSecret_CompressesLargeConfigUnderLimit(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()
	largeConfig := largeWorkerConfigYAML(t)

	require.Greater(t, len(largeConfig), 32768,
		"test fixture must exceed Hetzner's raw user_data limit to be representative")

	_, err := talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(), clientset, "123456789", largeConfig,
	)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().Secrets("kube-system").Get(
		context.Background(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)

	userData := autoscalerUserData(t, secret.Data["hcloud_cloud_init"])

	assert.LessOrEqual(t, len(userData), 32768,
		"gzip must bring user_data under Hetzner's 32 KiB limit")
	assert.True(t, isASCII(userData),
		"user_data must be ASCII so hcloud-go JSON marshaling does not corrupt it")
	assert.Equal(t, largeConfig, decodeAutoscalerCloudInit(t, secret.Data["hcloud_cloud_init"]),
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
		context.Background(), clientset, "123456789", incompressible,
	)
	require.ErrorIs(t, err, talosprovisioner.ErrAutoscalerUserDataTooLarge)

	// The guard must fire before any Secret is written.
	_, getErr := clientset.CoreV1().Secrets("kube-system").Get(
		context.Background(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	assert.True(t, apierrors.IsNotFound(getErr))
}
