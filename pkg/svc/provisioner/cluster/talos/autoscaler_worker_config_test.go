package talosprovisioner_test

import (
	"context"
	"encoding/base64"
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

// --- GenerateAutoscalerWorkerConfig ---

func TestGenerateAutoscalerWorkerConfig_StrippingLogic(t *testing.T) {
	t.Parallel()

	result, err := talosprovisioner.GenerateAutoscalerWorkerConfig(newTestWorkerProvider())
	require.NoError(t, err)
	require.NotEmpty(t, result)

	parsed, err := configloader.NewFromBytes(result)
	require.NoError(t, err)

	rawCfg := parsed.RawV1Alpha1()
	require.NotNil(t, rawCfg)
	require.NotNil(t, rawCfg.MachineConfig)

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
}

func TestApplyAutoscalerConfigSecret_CreatesNewSecret(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()
	snapshotID := "123456789"
	workerConfig := []byte("machine:\n  type: worker\n")

	err := talosprovisioner.ApplyAutoscalerConfigSecret(
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

	wantEncoded := base64.StdEncoding.EncodeToString(workerConfig)
	assert.Equal(t, []byte(wantEncoded), secret.Data["hcloud_cloud_init"])
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

	err := talosprovisioner.ApplyAutoscalerConfigSecret(
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

	wantEncoded := base64.StdEncoding.EncodeToString(workerConfig)
	assert.Equal(t, []byte(wantEncoded), secret.Data["hcloud_cloud_init"])

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

	err := talosprovisioner.ApplyAutoscalerConfigSecret(
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

	wantEncoded := base64.StdEncoding.EncodeToString(workerConfig)
	assert.Equal(t, []byte(wantEncoded), secret.Data["hcloud_cloud_init"])
}

func TestApplyAutoscalerConfigSecret_CreateConflictFallsBackToUpdate(t *testing.T) {
	t.Parallel()

	// Pre-create the secret so subsequent Gets (after AlreadyExists) succeed.
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
	clientset := fake.NewClientset(preExisting)

	getCallCount := 0

	clientset.PrependReactor(
		"get",
		"secrets",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			getCallCount++
			if getCallCount == 1 {
				// First Get: return NotFound to enter the create path.
				return true, nil, apierrors.NewNotFound(
					schema.GroupResource{Group: "", Resource: "secrets"},
					"cluster-autoscaler-config",
				)
			}

			// Subsequent Gets: pass through to the fake clientset.
			return false, nil, nil
		},
	)

	// Simulate a concurrent caller that created the Secret first.
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

	err := talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(),
		clientset,
		"new-image",
		[]byte("new-config"),
	)
	require.NoError(t, err)
}
