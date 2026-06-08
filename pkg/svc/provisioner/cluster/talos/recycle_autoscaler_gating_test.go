package talosprovisioner_test

import (
	"context"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// inPlaceDiff returns a diff carrying only an in-place change — the common config
// drift case that must NOT recycle autoscaler nodes.
func inPlaceDiff() *clusterupdate.UpdateResult {
	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field:    "machine.config",
		Category: clusterupdate.ChangeCategoryInPlace,
	})

	return diff
}

func TestAutoscalerRecycleRequired(t *testing.T) {
	t.Parallel()

	rebootDiff := clusterupdate.NewEmptyUpdateResult()
	rebootDiff.RebootRequired = append(rebootDiff.RebootRequired, clusterupdate.Change{})

	wipeDiff := clusterupdate.NewEmptyUpdateResult()
	wipeDiff.WipeRequired = append(wipeDiff.WipeRequired, clusterupdate.Change{})

	recreateDiff := clusterupdate.NewEmptyUpdateResult()
	recreateDiff.RecreateRequired = append(recreateDiff.RecreateRequired, clusterupdate.Change{})

	rollingDiff := clusterupdate.NewEmptyUpdateResult()
	rollingDiff.RollingRecreate = append(rollingDiff.RollingRecreate, clusterupdate.Change{})

	tests := []struct {
		name         string
		diff         *clusterupdate.UpdateResult
		imageChanged bool
		want         bool
	}{
		{"in-place only does not recycle", inPlaceDiff(), false, false},
		{"boot image change forces recycle", inPlaceDiff(), true, true},
		{"reboot-required recycles", rebootDiff, false, true},
		{"wipe-required recycles", wipeDiff, false, true},
		{"recreate-required recycles", recreateDiff, false, true},
		{"rolling-recreate recycles", rollingDiff, false, true},
		{"nil diff without image change does not recycle", nil, false, false},
		{"nil diff with image change recycles", nil, true, true},
		{"empty diff does not recycle", clusterupdate.NewEmptyUpdateResult(), false, false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := talosprovisioner.AutoscalerRecycleRequiredForTest(
				testCase.diff, testCase.imageChanged,
			)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestSnapshotImageIDFromSecret_RoundTrip(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()
	snapshotID := "987654321"

	_, err := talosprovisioner.ApplyAutoscalerConfigSecret(
		context.Background(),
		clientset,
		snapshotID,
		singlePoolConfig([]byte("machine:\n  type: worker\n")),
	)
	require.NoError(t, err)

	secret, err := clientset.CoreV1().Secrets("kube-system").Get(
		context.Background(), "cluster-autoscaler-config", metav1.GetOptions{},
	)
	require.NoError(t, err)

	assert.Equal(t, snapshotID, talosprovisioner.SnapshotImageIDFromSecretForTest(secret))
}

func TestSnapshotImageIDFromSecret_UnreadableReturnsEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		secret *corev1.Secret
	}{
		{"nil data", &corev1.Secret{}},
		{
			"missing key",
			&corev1.Secret{Data: map[string][]byte{"other": []byte("x")}},
		},
		{
			"invalid base64",
			&corev1.Secret{Data: map[string][]byte{
				clusterConfigSecretKey: []byte("not base64!!"),
			}},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Empty(t, talosprovisioner.SnapshotImageIDFromSecretForTest(testCase.secret))
		})
	}
}

func TestCurrentAutoscalerSnapshotImageID_NoKubeconfigReturnsEmpty(t *testing.T) {
	t.Parallel()

	// With no kubeconfig configured, newSecretKubeclient errors and the probe
	// degrades to "" — treated as "no detectable image change" by callers.
	prov := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)

	assert.Empty(t, prov.CurrentAutoscalerSnapshotImageIDForTest(context.Background()))
}

func TestApplyInPlaceToAutoscalerNodes_NoopWhenNotHetzner(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)

	err := prov.ApplyInPlaceToAutoscalerNodesForTest(
		context.Background(), "test-cluster", clusterupdate.NewEmptyUpdateResult(),
	)
	require.NoError(t, err)
}

func TestApplyInPlaceToAutoscalerNodes_NoopWhenAutoscalerDisabled(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled:   false,
			AutoscalerNodePoolNames: []string{"pool-a"},
		})

	err := prov.ApplyInPlaceToAutoscalerNodesForTest(
		context.Background(), "test-cluster", clusterupdate.NewEmptyUpdateResult(),
	)
	require.NoError(t, err)
}

func TestApplyInPlaceToAutoscalerNodes_NoopWhenNoPools(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled:   true,
			AutoscalerNodePoolNames: nil,
		})

	err := prov.ApplyInPlaceToAutoscalerNodesForTest(
		context.Background(), "test-cluster", clusterupdate.NewEmptyUpdateResult(),
	)
	require.NoError(t, err)
}
