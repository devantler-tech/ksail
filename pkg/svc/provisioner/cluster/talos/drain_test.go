package talosprovisioner_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// errDrainListBoom is a sentinel injected via a fake-clientset reactor to force a
// deterministic drain failure (the pod list errors) without a 10-minute hang.
var errDrainListBoom = errors.New("boom: list pods failed")

// defaultDrainTimeoutForTest mirrors the unexported defaultDrainTimeout constant.
const defaultDrainTimeoutForTest = 10 * time.Minute

func TestNewDrainHelper_StaticOptions(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()
	helper := talosprovisioner.NewDrainHelperForTest(
		context.Background(), clientset, 7*time.Minute, false, io.Discard,
	)

	assert.True(t, helper.Force, "Force should be set so bare pods are drained")
	assert.True(t, helper.IgnoreAllDaemonSets, "DaemonSet pods should be ignored")
	assert.True(t, helper.DeleteEmptyDirData, "emptyDir-backed pods should be deletable")
	assert.Equal(t, 7*time.Minute, helper.Timeout, "timeout should be threaded through")
	assert.Equal(t, -1, helper.GracePeriodSeconds, "should use pod's terminationGracePeriodSeconds")
	assert.Positive(t, helper.SkipWaitForDeleteTimeoutSeconds,
		"already-terminating pods should not block the whole drain")
}

func TestNewDrainHelper_DisableEviction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		disableEviction bool
	}{
		{name: "graceful eviction respects PodDisruptionBudgets", disableEviction: false},
		{name: "force deletes pods bypassing PodDisruptionBudgets", disableEviction: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			helper := talosprovisioner.NewDrainHelperForTest(
				context.Background(), fake.NewClientset(),
				time.Minute, testCase.disableEviction, io.Discard,
			)

			assert.Equal(t, testCase.disableEviction, helper.DisableEviction)
		})
	}
}

func TestDrainTimeout_FallsBackToDefaultWhenUnset(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, talosprovisioner.NewOptions())

	assert.Equal(t, defaultDrainTimeoutForTest, prov.DrainTimeoutForTest())
}

func TestDrainTimeout_UsesConfiguredValue(t *testing.T) {
	t.Parallel()

	opts := talosprovisioner.NewOptions().WithDrainTimeout(15 * time.Minute)
	prov := talosprovisioner.NewProvisioner(nil, opts)

	assert.Equal(t, 15*time.Minute, prov.DrainTimeoutForTest())
}

func TestWithDrainTimeout_IgnoresNonPositive(t *testing.T) {
	t.Parallel()

	opts := talosprovisioner.NewOptions().WithDrainTimeout(0).WithDrainTimeout(-time.Second)
	prov := talosprovisioner.NewProvisioner(nil, opts)

	// Non-positive overrides are ignored, so the provisioner still uses the default.
	assert.Equal(t, defaultDrainTimeoutForTest, prov.DrainTimeoutForTest())
}

func TestCordonAndDrain_Success_LeavesNodeCordoned(t *testing.T) {
	t.Parallel()

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-ok"}}
	clientset := fake.NewClientset(node)

	prov := talosprovisioner.NewProvisioner(nil, talosprovisioner.NewOptions()).
		WithLogWriter(io.Discard)

	ctx := context.Background()

	err := prov.CordonAndDrainForTest(ctx, clientset, "node-ok")
	require.NoError(t, err)

	got, getErr := clientset.CoreV1().Nodes().Get(ctx, "node-ok", metav1.GetOptions{})
	require.NoError(t, getErr)
	assert.True(t, got.Spec.Unschedulable,
		"a successful drain should leave the node cordoned for removal")
}

func TestCordonAndDrain_UncordonsOnDrainFailure(t *testing.T) {
	t.Parallel()

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-fail"}}
	clientset := fake.NewClientset(node)

	// Force the drain's pod enumeration to fail so the drain aborts deterministically.
	clientset.PrependReactor("list", "pods",
		func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, errDrainListBoom
		})

	prov := talosprovisioner.NewProvisioner(nil, talosprovisioner.NewOptions()).
		WithLogWriter(io.Discard)

	ctx := context.Background()

	err := prov.CordonAndDrainForTest(ctx, clientset, "node-fail")
	require.Error(t, err, "drain failure should propagate")

	got, getErr := clientset.CoreV1().Nodes().Get(ctx, "node-fail", metav1.GetOptions{})
	require.NoError(t, getErr)
	assert.False(t, got.Spec.Unschedulable,
		"a failed drain should uncordon the node so it stays schedulable")
}
