package readiness_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	statefulSetNamespace = "argocd"
	statefulSetName      = "argocd-application-controller"
	statefulSetShortWait = 150 * time.Millisecond
)

var errStatefulSetAPI = errors.New("apiserver unavailable")

// statefulSetFixture builds a StatefulSet whose controller has observed the
// current generation, with the given replica counters and update revision.
func statefulSetFixture(replicas, ready, updated int32, update string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: statefulSetName, Namespace: statefulSetNamespace, Generation: 1,
		},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 1,
			Replicas:           replicas,
			ReadyReplicas:      ready,
			UpdatedReplicas:    updated,
			CurrentRevision:    "rev-1",
			UpdateRevision:     update,
		},
	}
}

func readyStatefulSet() *appsv1.StatefulSet {
	return statefulSetFixture(1, 1, 1, "rev-1")
}

func waitIfExists(t *testing.T, objects ...runtime.Object) error {
	t.Helper()

	client := fake.NewClientset(objects...)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := readiness.WaitForStatefulSetReadyIfExists(
		ctx, client, statefulSetNamespace, statefulSetName, statefulSetShortWait,
	)
	if err != nil {
		return fmt.Errorf("wait for test statefulset: %w", err)
	}

	return nil
}

// TestWaitForStatefulSetReadyIfExists_Absent verifies an absent StatefulSet is
// nothing to wait for: a renamed or custom install must not stall the caller.
func TestWaitForStatefulSetReadyIfExists_Absent(t *testing.T) {
	t.Parallel()

	require.NoError(t, waitIfExists(t), "absent statefulset must be tolerated")
}

// TestWaitForStatefulSetReadyIfExists_Ready verifies the healthy shape reads as
// ready: current generation observed, every replica Ready, revisions converged.
func TestWaitForStatefulSetReadyIfExists_Ready(t *testing.T) {
	t.Parallel()

	require.NoError(t, waitIfExists(t, readyStatefulSet()))
}

// TestWaitForStatefulSetReadyIfExists_NotReady verifies a StatefulSet whose pods
// are not yet Ready is reported, which is the cold-start window the ArgoCD gate
// exists to wait out.
func TestWaitForStatefulSetReadyIfExists_NotReady(t *testing.T) {
	t.Parallel()

	err := waitIfExists(t, statefulSetFixture(1, 0, 1, "rev-1"))

	require.Error(t, err, "a statefulset with no Ready replicas must not read as ready")
}

// TestWaitForStatefulSetReadyIfExists_StaleObservedGeneration verifies that
// healthy-looking counters from a previous generation are not trusted before the
// controller has observed the current spec.
func TestWaitForStatefulSetReadyIfExists_StaleObservedGeneration(t *testing.T) {
	t.Parallel()

	stale := readyStatefulSet()
	stale.Generation = 2
	stale.Status.ObservedGeneration = 1

	err := waitIfExists(t, stale)

	require.Error(t, err, "stale observedGeneration must not read as ready")
}

// TestWaitForStatefulSetReadyIfExists_RollingUpdateInProgress verifies that a
// rollout still moving pods to the update revision is not ready even though every
// pod is currently Ready — the old-revision pods are about to be replaced.
func TestWaitForStatefulSetReadyIfExists_RollingUpdateInProgress(t *testing.T) {
	t.Parallel()

	err := waitIfExists(t, statefulSetFixture(2, 2, 1, "rev-2"))

	require.Error(t, err, "diverged revisions under RollingUpdate must not read as ready")
}

// TestWaitForStatefulSetReadyIfExists_OnDeleteIgnoresRevisions verifies that the
// OnDelete strategy, where the operator decides when pods move revision, does not
// hold readiness hostage to a revision that will never converge on its own.
func TestWaitForStatefulSetReadyIfExists_OnDeleteIgnoresRevisions(t *testing.T) {
	t.Parallel()

	onDelete := statefulSetFixture(2, 2, 0, "rev-2")
	onDelete.Spec.UpdateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType

	require.NoError(t, waitIfExists(t, onDelete))
}

// TestWaitForStatefulSetReadyIfExists_PartitionedRollout verifies that a
// partitioned RollingUpdate is complete once the ordinals above the partition run
// the update revision, and not before.
func TestWaitForStatefulSetReadyIfExists_PartitionedRollout(t *testing.T) {
	t.Parallel()

	partition := int32(1)

	complete := statefulSetFixture(3, 3, 2, "rev-2")
	complete.Spec.UpdateStrategy = appsv1.StatefulSetUpdateStrategy{
		Type:          appsv1.RollingUpdateStatefulSetStrategyType,
		RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{Partition: &partition},
	}

	require.NoError(t, waitIfExists(t, complete), "2 of 3 updated satisfies partition 1")

	incomplete := statefulSetFixture(3, 3, 1, "rev-2")
	incomplete.Spec.UpdateStrategy = complete.Spec.UpdateStrategy

	require.Error(t, waitIfExists(t, incomplete), "1 of 3 updated does not satisfy partition 1")
}

// TestWaitForStatefulSetReady_Ready verifies the non-IfExists variant polls the
// same readiness criteria.
func TestWaitForStatefulSetReady_Ready(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset(readyStatefulSet())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := readiness.WaitForStatefulSetReady(
		ctx, client, statefulSetNamespace, statefulSetName, time.Second,
	)

	require.NoError(t, err)
}

// TestWaitForStatefulSetReady_APIErrorPropagates verifies that a non-NotFound API
// error is surfaced immediately rather than polled until the deadline, so an
// unreachable API server is reported as such and not as "not ready".
func TestWaitForStatefulSetReady_APIErrorPropagates(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	client.PrependReactor(
		"get", "statefulsets",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errStatefulSetAPI
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := readiness.WaitForStatefulSetReadyIfExists(
		ctx, client, statefulSetNamespace, statefulSetName, time.Second,
	)

	require.ErrorIs(t, err, errStatefulSetAPI)
	assert.Contains(
		t,
		err.Error(),
		"failed to check statefulset argocd/argocd-application-controller",
	)
}
