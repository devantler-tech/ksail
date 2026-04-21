package readiness_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWaitForNodeReady_NodeAlreadyReady(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	})

	err := readiness.WaitForNodeReady(context.Background(), clientset, 5*time.Second)
	require.NoError(t, err)
}

func TestWaitForNodeReady_NodeNotReady(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
			},
		},
	})

	err := readiness.WaitForNodeReady(context.Background(), clientset, 100*time.Millisecond)
	assert.Error(t, err)
}

func TestWaitForNodeReady_NoNodes(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()

	err := readiness.WaitForNodeReady(context.Background(), clientset, 100*time.Millisecond)
	assert.Error(t, err)
}

func TestWaitForNodeReady_ContextCancelled(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := readiness.WaitForNodeReady(ctx, clientset, 5*time.Second)
	assert.Error(t, err)
}

func TestWaitForAllNodesReady_AllReady(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "control-plane"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		},
	)

	err := readiness.WaitForAllNodesReady(context.Background(), clientset, 5*time.Second)
	require.NoError(t, err)
}

func TestWaitForAllNodesReady_OneNotReady(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "control-plane"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
				},
			},
		},
	)

	err := readiness.WaitForAllNodesReady(context.Background(), clientset, 100*time.Millisecond)
	assert.Error(t, err)
}

func TestWaitForAllNodesReady_NoNodes(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()

	err := readiness.WaitForAllNodesReady(context.Background(), clientset, 100*time.Millisecond)
	assert.Error(t, err)
}

func TestWaitForAllNodesReady_SingleNode(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "kind-control-plane"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	})

	err := readiness.WaitForAllNodesReady(context.Background(), clientset, 5*time.Second)
	require.NoError(t, err)
}

func TestWaitForAllNodesReady_ContextCancelled(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := readiness.WaitForAllNodesReady(ctx, clientset, 5*time.Second)
	assert.Error(t, err)
}

func TestWaitForAllNodesReadyAndSchedulable_AllReadyNoTaints(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "control-plane"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		},
	)

	err := readiness.WaitForAllNodesReadyAndSchedulable(
		context.Background(), clientset, 5*time.Second,
	)
	require.NoError(t, err)
}

func TestWaitForAllNodesReadyAndSchedulable_ControlPlaneTaintBlocks(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "kind-control-plane"},
			Spec: corev1.NodeSpec{
				Taints: []corev1.Taint{
					{
						Key:    "node-role.kubernetes.io/control-plane",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		},
	)

	err := readiness.WaitForAllNodesReadyAndSchedulable(
		context.Background(), clientset, 100*time.Millisecond,
	)
	assert.Error(t, err, "should time out when only node has NoSchedule taint")
}

func TestWaitForAllNodesReadyAndSchedulable_WorkerSchedulableControlPlaneTainted(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "control-plane"},
			Spec: corev1.NodeSpec{
				Taints: []corev1.Taint{
					{
						Key:    "node-role.kubernetes.io/control-plane",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		},
	)

	err := readiness.WaitForAllNodesReadyAndSchedulable(
		context.Background(), clientset, 5*time.Second,
	)
	require.NoError(t, err, "should succeed when worker is schedulable")
}

func TestWaitForAllNodesReadyAndSchedulable_NotReadyBlocks(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "control-plane"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
				},
			},
		},
	)

	err := readiness.WaitForAllNodesReadyAndSchedulable(
		context.Background(), clientset, 100*time.Millisecond,
	)
	assert.Error(t, err, "should time out when node is not ready")
}

func TestWaitForAllNodesReadyAndSchedulable_NoNodes(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()

	err := readiness.WaitForAllNodesReadyAndSchedulable(
		context.Background(), clientset, 100*time.Millisecond,
	)
	assert.Error(t, err)
}

func TestWaitForAllNodesReadyAndSchedulable_ContextCancelled(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := readiness.WaitForAllNodesReadyAndSchedulable(ctx, clientset, 5*time.Second)
	assert.Error(t, err)
}

func TestWaitForAllNodesReadyAndSchedulable_NoExecuteTaintBlocks(t *testing.T) {
	t.Parallel()

	// NoExecute taints block scheduling: the scheduler rejects new pods that
	// have no matching toleration, just as it does for NoSchedule.
	clientset := fake.NewClientset(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
			Spec: corev1.NodeSpec{
				Taints: []corev1.Taint{
					{
						Key:    "some-key",
						Effect: corev1.TaintEffectNoExecute,
					},
				},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		},
	)

	err := readiness.WaitForAllNodesReadyAndSchedulable(
		context.Background(), clientset, 100*time.Millisecond,
	)
	assert.Error(t, err, "should time out when only node has NoExecute taint")
}

func TestWaitForAllNodesReadyAndSchedulable_CordonedNodeBlocks(t *testing.T) {
	t.Parallel()

	// Cordoned nodes (spec.unschedulable=true) reject new pod scheduling.
	clientset := fake.NewClientset(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
			Spec: corev1.NodeSpec{
				Unschedulable: true,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		},
	)

	err := readiness.WaitForAllNodesReadyAndSchedulable(
		context.Background(), clientset, 100*time.Millisecond,
	)
	assert.Error(t, err, "should time out when only node is cordoned")
}
