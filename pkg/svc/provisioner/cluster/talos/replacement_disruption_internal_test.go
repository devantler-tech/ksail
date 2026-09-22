package talosprovisioner

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	drainNode      = "prod-control-plane-1"
	drainOtherNode = "prod-control-plane-2"
	drainNamespace = "apps"
)

// drainPod returns a running pod on node with labels in the shared test namespace.
func drainPod(name, node string, labels map[string]string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: drainNamespace, Labels: labels},
		Spec:       corev1.PodSpec{NodeName: node},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

// drainBudget returns a budget selecting match whose status is current and allows allowed disruptions.
func drainBudget(name string, match map[string]string, allowed int32) policyv1.PodDisruptionBudget {
	return policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: drainNamespace, Generation: 3},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: match},
		},
		Status: policyv1.PodDisruptionBudgetStatus{
			ObservedGeneration: 3,
			DisruptionsAllowed: allowed,
		},
	}
}

// webLabels returns the labels the web pods and budgets share.
func webLabels() map[string]string {
	return map[string]string{"app": "web"}
}

// TestProveDrainAllowedAcceptsABudgetThatAllowsADisruption verifies a pod whose only budget
// allows a disruption can be drained.
func TestProveDrainAllowedAcceptsABudgetThatAllowsADisruption(t *testing.T) {
	t.Parallel()

	pods := []corev1.Pod{drainPod("web-0", drainNode, webLabels())}
	budgets := []policyv1.PodDisruptionBudget{drainBudget("web", webLabels(), 1)}

	require.NoError(t, proveDrainAllowed(drainNode, pods, budgets))
}

// TestProveDrainAllowedRejectsABudgetThatAllowsNoDisruption verifies a budget allowing no
// disruption blocks the drain and the error names the pod and the budget.
func TestProveDrainAllowedRejectsABudgetThatAllowsNoDisruption(t *testing.T) {
	t.Parallel()

	pods := []corev1.Pod{drainPod("web-0", drainNode, webLabels())}
	budgets := []policyv1.PodDisruptionBudget{drainBudget("web", webLabels(), 0)}

	err := proveDrainAllowed(drainNode, pods, budgets)
	require.ErrorIs(t, err, ErrDrainBlockedByDisruptionBudget)
	require.ErrorContains(t, err, "apps/web-0")
	require.ErrorContains(t, err, "apps/web")
}

// TestProveDrainAllowedRejectsAPodSelectedByTwoBudgets verifies a pod two budgets select is
// refused, because the eviction API rejects it, and the error names both budgets.
func TestProveDrainAllowedRejectsAPodSelectedByTwoBudgets(t *testing.T) {
	t.Parallel()

	pods := []corev1.Pod{drainPod("web-0", drainNode, webLabels())}
	budgets := []policyv1.PodDisruptionBudget{
		drainBudget("web", webLabels(), 1),
		drainBudget("web-too", webLabels(), 1),
	}

	err := proveDrainAllowed(drainNode, pods, budgets)
	require.ErrorIs(t, err, ErrDrainBlockedByDisruptionBudget)
	require.ErrorContains(t, err, "apps/web-0")
	require.ErrorContains(t, err, "apps/web,")
	require.ErrorContains(t, err, "apps/web-too")
}

// TestProveDrainAllowedRejectsABudgetWhoseStatusIsStale verifies a budget whose status has not
// observed its current generation is refused rather than trusted.
func TestProveDrainAllowedRejectsABudgetWhoseStatusIsStale(t *testing.T) {
	t.Parallel()

	budget := drainBudget("web", webLabels(), 1)
	budget.Status.ObservedGeneration = budget.Generation - 1

	pods := []corev1.Pod{drainPod("web-0", drainNode, webLabels())}

	err := proveDrainAllowed(drainNode, pods, []policyv1.PodDisruptionBudget{budget})
	require.ErrorIs(t, err, ErrDrainBlockedByDisruptionBudget)
	require.ErrorContains(t, err, "apps/web-0")
	require.ErrorContains(t, err, "apps/web ")
}

// TestProveDrainAllowedRejectsABudgetWhoseStatusIsAhead verifies a status claiming a newer
// generation is refused too: Kubernetes trusts DisruptionsAllowed only when the observed
// generation equals the budget's generation.
func TestProveDrainAllowedRejectsABudgetWhoseStatusIsAhead(t *testing.T) {
	t.Parallel()

	budget := drainBudget("web", webLabels(), 1)
	budget.Status.ObservedGeneration = budget.Generation + 1

	pods := []corev1.Pod{drainPod("web-0", drainNode, webLabels())}

	require.ErrorIs(t,
		proveDrainAllowed(drainNode, pods, []policyv1.PodDisruptionBudget{budget}),
		ErrDrainBlockedByDisruptionBudget)
}

// TestProveDrainAllowedIgnoresPodsTheDrainDoesNotEvict verifies DaemonSet-owned, mirror,
// completed and other-node pods never block the drain.
func TestProveDrainAllowedIgnoresPodsTheDrainDoesNotEvict(t *testing.T) {
	t.Parallel()

	daemon := drainPod("agent-x", drainNode, webLabels())
	daemon.OwnerReferences = []metav1.OwnerReference{
		{Kind: "DaemonSet", Name: "agent", Controller: new(true)},
	}

	mirror := drainPod("static-x", drainNode, webLabels())
	mirror.Annotations = map[string]string{corev1.MirrorPodAnnotationKey: "hash"}

	done := drainPod("job-x", drainNode, webLabels())
	done.Status.Phase = corev1.PodSucceeded

	failed := drainPod("job-y", drainNode, webLabels())
	failed.Status.Phase = corev1.PodFailed

	elsewhere := drainPod("web-1", drainOtherNode, webLabels())

	pods := []corev1.Pod{daemon, mirror, done, failed, elsewhere}
	budgets := []policyv1.PodDisruptionBudget{drainBudget("web", webLabels(), 0)}

	require.NoError(t, proveDrainAllowed(drainNode, pods, budgets))
}

// TestProveDrainAllowedIgnoresBudgetsThatDoNotSelectThePod verifies budgets in another
// namespace or with a non-matching selector do not block the drain.
func TestProveDrainAllowedIgnoresBudgetsThatDoNotSelectThePod(t *testing.T) {
	t.Parallel()

	otherNamespace := drainBudget("web", webLabels(), 0)
	otherNamespace.Namespace = "elsewhere"

	pods := []corev1.Pod{drainPod("web-0", drainNode, webLabels())}
	budgets := []policyv1.PodDisruptionBudget{
		otherNamespace,
		drainBudget("db", map[string]string{"app": "db"}, 0),
	}

	require.NoError(t, proveDrainAllowed(drainNode, pods, budgets))
}

// TestProveDrainAllowedRejectsAnUnparseableSelector verifies a budget whose selector cannot be
// parsed is refused, naming the pod and the budget.
func TestProveDrainAllowedRejectsAnUnparseableSelector(t *testing.T) {
	t.Parallel()

	budget := drainBudget("web", nil, 1)
	budget.Spec.Selector = &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: "Bogus"}},
	}

	pods := []corev1.Pod{drainPod("web-0", drainNode, webLabels())}

	err := proveDrainAllowed(drainNode, pods, []policyv1.PodDisruptionBudget{budget})
	require.ErrorIs(t, err, ErrDrainBlockedByDisruptionBudget)
	require.ErrorContains(t, err, "apps/web-0")
	require.ErrorContains(t, err, "apps/web ")
}

// TestProveDrainAllowedJudgesOnlyTheNodeBeingDrained verifies only pods on the drained node are
// judged, so a blocked pod elsewhere is not reported.
func TestProveDrainAllowedJudgesOnlyTheNodeBeingDrained(t *testing.T) {
	t.Parallel()

	pods := []corev1.Pod{
		drainPod("web-0", drainNode, webLabels()),
		drainPod("web-1", drainOtherNode, webLabels()),
	}
	budgets := []policyv1.PodDisruptionBudget{drainBudget("web", webLabels(), 0)}

	err := proveDrainAllowed(drainOtherNode, pods, budgets)
	require.ErrorIs(t, err, ErrDrainBlockedByDisruptionBudget)
	require.ErrorContains(t, err, "apps/web-1")
	require.NotContains(t, err.Error(), "apps/web-0")
}
