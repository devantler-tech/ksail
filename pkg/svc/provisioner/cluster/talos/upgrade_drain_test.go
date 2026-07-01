package talosprovisioner_test

import (
	"context"
	"path/filepath"
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestUncordonAfterUpgradeUncordonsReadyNode asserts the graceful OS-upgrade path
// uncordons a node once it is back Ready — the counterpart to the cordon+drain it
// performs before the reboot.
func TestUncordonAfterUpgradeUncordonsReadyNode(t *testing.T) {
	t.Parallel()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-worker-1"},
		Spec:       corev1.NodeSpec{Unschedulable: true},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}
	clientset := fake.NewClientset(node)
	prov := talosprovisioner.NewProvisioner(nil, talosprovisioner.NewOptions())

	uncordonErr := prov.UncordonAfterUpgradeForTest(
		context.Background(),
		clientset,
		"prod-worker-1",
	)
	require.NoError(t, uncordonErr)

	got, err := clientset.CoreV1().
		Nodes().
		Get(context.Background(), "prod-worker-1", metav1.GetOptions{})
	require.NoError(t, err)
	assert.False(t, got.Spec.Unschedulable, "node should be uncordoned after a successful upgrade")
}

// TestUncordonAfterUpgradeGatesOnReadiness asserts that a node which never reports
// Ready after its upgrade fails the roll (so the next node is not rebooted while this
// one is still down) and is left cordoned rather than uncordoned-and-advanced.
func TestUncordonAfterUpgradeGatesOnReadiness(t *testing.T) {
	t.Parallel()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-worker-1"},
		Spec:       corev1.NodeSpec{Unschedulable: true},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
			},
		},
	}
	clientset := fake.NewClientset(node)
	prov := talosprovisioner.NewProvisioner(nil, talosprovisioner.NewOptions())

	// A cancelled context makes the readiness poll fail fast instead of blocking for
	// the full nodeReadinessTimeout, exercising the not-Ready gate path.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	uncordonErr := prov.UncordonAfterUpgradeForTest(ctx, clientset, "prod-worker-1")
	require.Error(t, uncordonErr, "a node that never reports Ready must fail the roll")

	got, err := clientset.CoreV1().
		Nodes().
		Get(context.Background(), "prod-worker-1", metav1.GetOptions{})
	require.NoError(t, err)
	assert.True(
		t,
		got.Spec.Unschedulable,
		"a node that never became Ready must stay cordoned, not be uncordoned-and-advanced",
	)
}

// TestK8sClientOrWarnForUpgradeNilOnUnreachableAPI asserts the upgrade roll degrades
// gracefully (returns a nil clientset → reboot-without-drain) when the Kubernetes
// API cannot be reached, instead of aborting a needed OS upgrade.
func TestK8sClientOrWarnForUpgradeNilOnUnreachableAPI(t *testing.T) {
	t.Parallel()

	badPath := filepath.Join(t.TempDir(), "does-not-exist", "kubeconfig")
	opts := talosprovisioner.NewOptions().WithKubeconfigPath(badPath)
	prov := talosprovisioner.NewProvisioner(nil, opts)

	got := prov.K8sClientOrWarnForUpgradeForTest("prod")

	assert.Nil(
		t,
		got,
		"an unreachable Kubernetes API should yield a nil clientset (degrade, not abort)",
	)
}
