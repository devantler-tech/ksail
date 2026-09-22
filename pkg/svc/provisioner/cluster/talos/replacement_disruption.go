package talosprovisioner

import (
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// ErrDrainBlockedByDisruptionBudget is returned when draining the replacement target would be
// refused by a PodDisruptionBudget, so the replacement must not start.
var ErrDrainBlockedByDisruptionBudget = errors.New("drain is blocked by a disruption budget")

// proveDrainAllowed succeeds only when every pod the drain of nodeName would evict can be
// evicted right now. The eviction API refuses a pod whose budget allows no disruption and any
// pod more than one budget selects, so either would leave the node cordoned with the drain
// stuck. A budget whose status has not observed its current spec, or whose selector cannot be
// parsed, cannot prove anything and is refused rather than trusted. Pods the drain skips
// (DaemonSet-owned, mirror and completed pods) are ignored.
//
// Allowing one disruption is the preflight bar, not a guarantee that every pod of a large
// budget is evicted at once: the drain evicts them in turn as the budget recovers.
func proveDrainAllowed(
	nodeName string,
	pods []corev1.Pod,
	budgets []policyv1.PodDisruptionBudget,
) error {
	for i := range pods {
		pod := &pods[i]
		if !drainEvicts(pod, nodeName) {
			continue
		}

		err := podEvictable(pod, budgets)
		if err != nil {
			return err
		}
	}

	return nil
}

// drainEvicts reports whether draining nodeName evicts pod.
func drainEvicts(pod *corev1.Pod, nodeName string) bool {
	if pod.Spec.NodeName != nodeName {
		return false
	}

	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return false
	}

	if _, mirror := pod.Annotations[corev1.MirrorPodAnnotationKey]; mirror {
		return false
	}

	owner := metav1.GetControllerOf(pod)

	return owner == nil || owner.Kind != "DaemonSet"
}

// podEvictable refuses pod when the budgets that select it would reject its eviction.
func podEvictable(pod *corev1.Pod, budgets []policyv1.PodDisruptionBudget) error {
	var selecting []*policyv1.PodDisruptionBudget

	for i := range budgets {
		budget := &budgets[i]
		if budget.Namespace != pod.Namespace {
			continue
		}

		selector, err := metav1.LabelSelectorAsSelector(budget.Spec.Selector)
		if err != nil {
			return fmt.Errorf("%w: pod %s/%s; budget %s/%s has an invalid selector: %w",
				ErrDrainBlockedByDisruptionBudget, pod.Namespace, pod.Name,
				budget.Namespace, budget.Name, err)
		}

		if selector.Matches(labels.Set(pod.Labels)) {
			selecting = append(selecting, budget)
		}
	}

	if len(selecting) > 1 {
		names := make([]string, 0, len(selecting))
		for _, budget := range selecting {
			names = append(names, budget.Namespace+"/"+budget.Name)
		}

		return fmt.Errorf("%w: pod %s/%s is selected by budgets %s and cannot be evicted",
			ErrDrainBlockedByDisruptionBudget, pod.Namespace, pod.Name, strings.Join(names, ", "))
	}

	if len(selecting) == 0 {
		return nil
	}

	budget := selecting[0]
	// Kubernetes trusts DisruptionsAllowed only when the status reflects exactly this generation.
	if budget.Status.ObservedGeneration != budget.Generation {
		return fmt.Errorf(
			"%w: pod %s/%s; budget %s/%s status is stale (observed generation %d of %d)",
			ErrDrainBlockedByDisruptionBudget,
			pod.Namespace,
			pod.Name,
			budget.Namespace,
			budget.Name,
			budget.Status.ObservedGeneration,
			budget.Generation,
		)
	}

	if budget.Status.DisruptionsAllowed < 1 {
		return fmt.Errorf("%w: budget %s/%s allows no disruption, so pod %s/%s cannot be evicted",
			ErrDrainBlockedByDisruptionBudget, budget.Namespace, budget.Name,
			pod.Namespace, pod.Name)
	}

	return nil
}
