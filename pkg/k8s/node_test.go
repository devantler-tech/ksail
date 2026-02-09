package k8s_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
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

	err := k8s.WaitForNodeReady(context.Background(), clientset, 5*time.Second)
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

	err := k8s.WaitForNodeReady(context.Background(), clientset, 3*time.Second)
	assert.Error(t, err)
}

func TestWaitForNodeReady_NoNodes(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()

	err := k8s.WaitForNodeReady(context.Background(), clientset, 3*time.Second)
	assert.Error(t, err)
}

func TestWaitForNodeReady_ContextCancelled(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := k8s.WaitForNodeReady(ctx, clientset, 5*time.Second)
	assert.Error(t, err)
}
