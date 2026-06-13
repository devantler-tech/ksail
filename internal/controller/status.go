package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// setCondition records a condition of the given type on the cluster status, observed at the
// cluster's current generation.
func setCondition(
	cluster *v1alpha1.Cluster,
	conditionType string,
	status metav1.ConditionStatus,
	reason, message string,
) {
	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: cluster.Generation,
		Reason:             reason,
		Message:            message,
	})
}

// fail records a failure on the cluster status and returns a requeue so reconciliation retries.
func (r *ClusterReconciler) fail(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	reason string,
	cause error,
) (ctrl.Result, error) {
	before := cluster.Status.DeepCopy()

	cluster.Status.Phase = v1alpha1.ClusterPhaseFailed
	cluster.Status.ObservedGeneration = cluster.Generation

	message := cause.Error()
	setCondition(cluster, v1alpha1.ConditionReady, metav1.ConditionFalse, reason, message)
	setCondition(cluster, v1alpha1.ConditionDegraded, metav1.ConditionTrue, reason, message)
	// Clear Progressing so a previously-Progressing cluster does not stay Progressing while Failed.
	setCondition(cluster, v1alpha1.ConditionProgressing, metav1.ConditionFalse, reason, message)

	statusErr := r.updateStatusIfChanged(ctx, cluster, before)
	if statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	return ctrl.Result{RequeueAfter: r.transitionalRequeue()}, nil
}

// markProgressing sets a transitional phase and the Progressing condition.
func (r *ClusterReconciler) markProgressing(
	cluster *v1alpha1.Cluster,
	phase v1alpha1.ClusterPhase,
	reason, message string,
) {
	cluster.Status.Phase = phase
	setCondition(cluster, v1alpha1.ConditionProgressing, metav1.ConditionTrue, reason, message)
}

// markReady records a successful reconciliation. LastReconcileTime is set by
// updateStatusIfChanged only when the status actually changes, to avoid status-write churn.
func (r *ClusterReconciler) markReady(cluster *v1alpha1.Cluster) {
	cluster.Status.Phase = v1alpha1.ClusterPhaseReady
	cluster.Status.ObservedGeneration = cluster.Generation

	const message = "Cluster is reconciled and ready"

	setCondition(cluster, v1alpha1.ConditionReady, metav1.ConditionTrue, reasonReconciled, message)
	setCondition(
		cluster,
		v1alpha1.ConditionProgressing,
		metav1.ConditionFalse,
		reasonReconciled,
		message,
	)
	setCondition(
		cluster,
		v1alpha1.ConditionDegraded,
		metav1.ConditionFalse,
		reasonReconciled,
		message,
	)
}

// updateStatusIfChanged persists the status only when it differs from before (ignoring
// LastReconcileTime). This avoids a tight reconcile loop where every steady-state reconcile would
// otherwise write status, generate a watch event, and trigger another reconcile. LastReconcileTime
// is stamped only when a real change is being written.
func (r *ClusterReconciler) updateStatusIfChanged(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	before *v1alpha1.ClusterStatus,
) error {
	currentCmp := cluster.Status.DeepCopy()
	beforeCmp := before.DeepCopy()
	currentCmp.LastReconcileTime = nil
	beforeCmp.LastReconcileTime = nil

	if equality.Semantic.DeepEqual(beforeCmp, currentCmp) {
		return nil
	}

	now := metav1.Now()
	cluster.Status.LastReconcileTime = &now

	return r.updateStatus(ctx, cluster)
}

// updateStatus persists the cluster status subresource.
func (r *ClusterReconciler) updateStatus(ctx context.Context, cluster *v1alpha1.Cluster) error {
	err := r.Status().Update(ctx, cluster)
	if err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	return nil
}

func (r *ClusterReconciler) readyRequeue() time.Duration {
	if r.ReadyRequeue > 0 {
		return r.ReadyRequeue
	}

	return defaultReadyRequeue
}

func (r *ClusterReconciler) transitionalRequeue() time.Duration {
	if r.TransitionalRequeue > 0 {
		return r.TransitionalRequeue
	}

	return defaultTransitionalRequeue
}
