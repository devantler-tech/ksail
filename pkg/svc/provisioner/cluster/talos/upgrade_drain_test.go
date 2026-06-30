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

	prov.UncordonAfterUpgradeForTest(context.Background(), clientset, "prod-worker-1")

	got, err := clientset.CoreV1().
		Nodes().
		Get(context.Background(), "prod-worker-1", metav1.GetOptions{})
	require.NoError(t, err)
	assert.False(t, got.Spec.Unschedulable, "node should be uncordoned after a successful upgrade")
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
