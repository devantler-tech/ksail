package talosprovisioner_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	autoscalerNamespace      = "kube-system"
	autoscalerDeploymentName = "cluster-autoscaler"
)

func TestSortServersByName(t *testing.T) {
	t.Parallel()

	servers := []*hcloud.Server{
		{Name: "as-cx33-2"},
		{Name: "as-cx23-1"},
		{Name: "as-cx33-1"},
	}

	ordered := talosprovisioner.SortServersByNameForTest(servers)

	names := make([]string, 0, len(ordered))
	for _, server := range ordered {
		names = append(names, server.Name)
	}

	assert.Equal(t, []string{"as-cx23-1", "as-cx33-1", "as-cx33-2"}, names)
	// Input slice order must be preserved (sort operates on a clone).
	assert.Equal(t, "as-cx33-2", servers[0].Name)
}

func TestRecycleAutoscalerNodes_NoopWhenNotHetzner(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)

	err := prov.RecycleAutoscalerNodesForTest(context.Background(), "test-cluster")
	require.NoError(t, err)
}

func TestRecycleAutoscalerNodes_NoopWhenAutoscalerDisabled(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled:   false,
			AutoscalerNodePoolNames: []string{"pool-a"},
		})

	err := prov.RecycleAutoscalerNodesForTest(context.Background(), "test-cluster")
	require.NoError(t, err)
}

func TestRecycleAutoscalerNodes_NoopWhenNoPools(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled:   true,
			AutoscalerNodePoolNames: nil,
		})

	err := prov.RecycleAutoscalerNodesForTest(context.Background(), "test-cluster")
	require.NoError(t, err)
}

// autoscalerDeployment builds a cluster-autoscaler Deployment with the standard
// instance label. When ready, its status reports one updated, available replica.
func autoscalerDeployment(ready bool) *appsv1.Deployment {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      autoscalerDeploymentName,
			Namespace: autoscalerNamespace,
			Labels:    map[string]string{"app.kubernetes.io/instance": autoscalerDeploymentName},
		},
	}

	if ready {
		deployment.Status = appsv1.DeploymentStatus{
			Replicas:          1,
			UpdatedReplicas:   1,
			AvailableReplicas: 1,
		}
	}

	return deployment
}

func TestWaitForAutoscalerRollout_ReadyDeployment(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)
	clientset := fake.NewClientset(autoscalerDeployment(true))

	err := prov.WaitForAutoscalerRolloutForTest(context.Background(), clientset)
	require.NoError(t, err)
}

func TestWaitForAutoscalerRollout_NoDeploymentIsNoop(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)

	err := prov.WaitForAutoscalerRolloutForTest(context.Background(), fake.NewClientset())
	require.NoError(t, err)
}

func TestWaitForAutoscalerRollout_NotReadyTimesOut(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)
	clientset := fake.NewClientset(autoscalerDeployment(false))

	// A short caller deadline bounds the wait so the never-ready Deployment surfaces
	// an error promptly instead of blocking on the full rollout timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := prov.WaitForAutoscalerRolloutForTest(ctx, clientset)
	require.Error(t, err)
}
