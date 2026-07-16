package readiness_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestWaitForDeploymentReadyIfExists_StaleObservedGeneration verifies that a
// Deployment whose Status still reflects a previous generation (ObservedGeneration
// < Generation) is NOT treated as ready even when its replica counters look
// healthy — the controller has not yet observed the current spec (e.g. right after
// an upgrade), so those counters describe the old rollout.
func TestWaitForDeploymentReadyIfExists_StaleObservedGeneration(t *testing.T) {
	t.Parallel()

	const namespace, name = "argocd", "argocd-repo-server"

	client := fake.NewClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Generation: 2},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1, // controller has not observed generation 2 yet
			Replicas:           1,
			UpdatedReplicas:    1,
			AvailableReplicas:  1,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := readiness.WaitForDeploymentReadyIfExists(
		ctx, client, namespace, name, 150*time.Millisecond,
	)

	require.Error(t, err, "stale observedGeneration must not read as ready")
}

// TestWaitForDeploymentReadyIfExists_ObservedGenerationCaughtUp verifies the same
// Deployment reads as ready once the controller has observed the current
// generation and the replica counters are healthy.
func TestWaitForDeploymentReadyIfExists_ObservedGenerationCaughtUp(t *testing.T) {
	t.Parallel()

	const namespace, name = "argocd", "argocd-repo-server"

	client := fake.NewClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Generation: 2},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 2,
			Replicas:           1,
			UpdatedReplicas:    1,
			AvailableReplicas:  1,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := readiness.WaitForDeploymentReadyIfExists(
		ctx, client, namespace, name, time.Second,
	)

	require.NoError(t, err, "current-generation ready deployment must read as ready")
}
