package k8s_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestWaitForMultipleResources_EmptyChecks tests handling of empty checks slice.
func TestWaitForMultipleResources_EmptyChecks(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	ctx := context.Background()

	err := k8s.WaitForMultipleResources(ctx, client, []k8s.ReadinessCheck{}, 100*time.Millisecond)

	require.NoError(t, err, "should succeed with empty checks")
}

// TestWaitForMultipleResources_SingleDeploymentReady tests single ready deployment.
func TestWaitForMultipleResources_SingleDeploymentReady(t *testing.T) {
	t.Parallel()

	const (
		namespace = "test-system"
		name      = "test-deployment"
	)

	client := fake.NewClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: appsv1.DeploymentStatus{
			Replicas:          1,
			UpdatedReplicas:   1,
			AvailableReplicas: 1,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	checks := []k8s.ReadinessCheck{
		{Type: "deployment", Namespace: namespace, Name: name},
	}

	err := k8s.WaitForMultipleResources(ctx, client, checks, 500*time.Millisecond)

	require.NoError(t, err)
}

// TestWaitForMultipleResources_SingleDaemonSetReady tests single ready daemonset.
func TestWaitForMultipleResources_SingleDaemonSetReady(t *testing.T) {
	t.Parallel()

	const (
		namespace = "kube-system"
		name      = "test-daemon"
	)

	client := fake.NewClientset(&appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: 1,
			NumberUnavailable:      0,
			UpdatedNumberScheduled: 1,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	checks := []k8s.ReadinessCheck{
		{Type: "daemonset", Namespace: namespace, Name: name},
	}

	err := k8s.WaitForMultipleResources(ctx, client, checks, 500*time.Millisecond)

	require.NoError(t, err)
}

// TestWaitForMultipleResources_MultipleResources tests multiple resources becoming ready.
func TestWaitForMultipleResources_MultipleResources(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "deploy1", Namespace: "ns1"},
			Status: appsv1.DeploymentStatus{
				Replicas:          1,
				UpdatedReplicas:   1,
				AvailableReplicas: 1,
			},
		},
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "ds1", Namespace: "ns2"},
			Status: appsv1.DaemonSetStatus{
				DesiredNumberScheduled: 1,
				NumberUnavailable:      0,
				UpdatedNumberScheduled: 1,
			},
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	checks := []k8s.ReadinessCheck{
		{Type: "deployment", Namespace: "ns1", Name: "deploy1"},
		{Type: "daemonset", Namespace: "ns2", Name: "ds1"},
	}

	err := k8s.WaitForMultipleResources(ctx, client, checks, 1*time.Second)

	require.NoError(t, err)
}

// TestWaitForMultipleResources_UnknownResourceType tests handling of unknown resource types.
func TestWaitForMultipleResources_UnknownResourceType(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	ctx := context.Background()

	checks := []k8s.ReadinessCheck{
		{Type: "unknown", Namespace: "ns", Name: "resource"},
	}

	err := k8s.WaitForMultipleResources(ctx, client, checks, 100*time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown resource type")
	assert.Contains(t, err.Error(), "unknown")
}

// TestWaitForMultipleResources_ResourceNotReady tests timeout when resource is not ready.
func TestWaitForMultipleResources_ResourceNotReady(t *testing.T) {
	t.Parallel()

	const (
		namespace = "test-ns"
		name      = "not-ready-deploy"
	)

	// Deployment with mismatched replicas (not ready)
	client := fake.NewClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: appsv1.DeploymentStatus{
			Replicas:          2,
			UpdatedReplicas:   1, // Only 1 of 2 updated
			AvailableReplicas: 0,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	checks := []k8s.ReadinessCheck{
		{Type: "deployment", Namespace: namespace, Name: name},
	}

	err := k8s.WaitForMultipleResources(ctx, client, checks, 200*time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready")
}

// TestWaitForMultipleResources_FirstResourceFails tests failure on first resource.
func TestWaitForMultipleResources_FirstResourceFails(t *testing.T) {
	t.Parallel()

	// Second resource is ready, but first will fail
	client := fake.NewClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "second-deploy", Namespace: "ns"},
		Status: appsv1.DeploymentStatus{
			Replicas:          1,
			UpdatedReplicas:   1,
			AvailableReplicas: 1,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	checks := []k8s.ReadinessCheck{
		{Type: "deployment", Namespace: "ns", Name: "missing-deploy"}, // Doesn't exist
		{Type: "deployment", Namespace: "ns", Name: "second-deploy"},  // Exists
	}

	err := k8s.WaitForMultipleResources(ctx, client, checks, 200*time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing-deploy")
}

// TestReadinessCheck_Fields tests the ReadinessCheck struct fields.
func TestReadinessCheck_Fields(t *testing.T) {
	t.Parallel()

	check := k8s.ReadinessCheck{
		Type:      "deployment",
		Namespace: "my-namespace",
		Name:      "my-deployment",
	}

	assert.Equal(t, "deployment", check.Type)
	assert.Equal(t, "my-namespace", check.Namespace)
	assert.Equal(t, "my-deployment", check.Name)
}

// TestWaitForMultipleResources_TimeoutExceeded tests the timeout exceeded error.
func TestWaitForMultipleResources_TimeoutExceeded(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()

	// Use a zero-timeout context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	checks := []k8s.ReadinessCheck{
		{Type: "deployment", Namespace: "ns", Name: "deploy"},
	}

	// With 0 timeout, the error message should be about timeout
	err := k8s.WaitForMultipleResources(ctx, client, checks, 0)

	require.Error(t, err)
	assert.ErrorIs(t, err, k8s.ErrTimeoutExceeded)
}

// TestWaitForMultipleResources_MixedTypes tests mixed deployment and daemonset.
func TestWaitForMultipleResources_MixedTypes(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "coredns", Namespace: "kube-system"},
			Status: appsv1.DeploymentStatus{
				Replicas:          2,
				UpdatedReplicas:   2,
				AvailableReplicas: 2,
			},
		},
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "cilium", Namespace: "kube-system"},
			Status: appsv1.DaemonSetStatus{
				DesiredNumberScheduled: 3,
				NumberUnavailable:      0,
				UpdatedNumberScheduled: 3,
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "traefik", Namespace: "traefik"},
			Status: appsv1.DeploymentStatus{
				Replicas:          1,
				UpdatedReplicas:   1,
				AvailableReplicas: 1,
			},
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	checks := []k8s.ReadinessCheck{
		{Type: "deployment", Namespace: "kube-system", Name: "coredns"},
		{Type: "daemonset", Namespace: "kube-system", Name: "cilium"},
		{Type: "deployment", Namespace: "traefik", Name: "traefik"},
	}

	err := k8s.WaitForMultipleResources(ctx, client, checks, 1*time.Second)

	require.NoError(t, err)
}
