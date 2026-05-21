// Package controller contains the controller-runtime reconcilers for the KSail operator.
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// FinalizerName is added to Cluster resources so the operator can tear down the underlying
// cluster before the custom resource is removed from the API server.
const FinalizerName = "ksail.io/finalizer"

// LastAppliedSpecAnnotation stores the JSON of the cluster spec the operator last provisioned,
// used as the baseline for drift detection on subsequent reconciles.
const LastAppliedSpecAnnotation = "ksail.io/last-applied-spec"

// reasonReconciled is the condition reason reported when a cluster is reconciled and ready.
const reasonReconciled = "Reconciled"

// Default requeue intervals. Transitional states are requeued quickly so the reported phase
// converges promptly; steady (Ready) state is polled less frequently.
const (
	defaultReadyRequeue        = 60 * time.Second
	defaultTransitionalRequeue = 10 * time.Second
)

// ProvisionerBuilder returns a distribution provisioner for the given Cluster. The operator
// supplies a builder backed by the existing provisioner factory (forcing the Kubernetes
// provider so clusters are provisioned in-cluster); tests supply a fake.
type ProvisionerBuilder func(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, error)

// ClusterReconciler reconciles a Cluster object towards its desired state.
type ClusterReconciler struct {
	client.Client

	Scheme *runtime.Scheme

	// NewProvisioner builds the provisioner used to create/delete the underlying cluster.
	NewProvisioner ProvisionerBuilder

	// ReadyRequeue overrides the steady-state requeue interval (zero uses the default).
	ReadyRequeue time.Duration
	// TransitionalRequeue overrides the transitional requeue interval (zero uses the default).
	TransitionalRequeue time.Duration
}

// +kubebuilder:rbac:groups=ksail.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ksail.io,resources=clusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ksail.io,resources=clusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives a single Cluster towards its desired state.
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var cluster v1alpha1.Cluster

	err := r.Get(ctx, req.NamespacedName, &cluster)
	if apierrors.IsNotFound(err) {
		return ctrl.Result{}, nil
	}

	if err != nil {
		return ctrl.Result{}, fmt.Errorf("get cluster: %w", err)
	}

	if !cluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &cluster)
	}

	if controllerutil.AddFinalizer(&cluster, FinalizerName) {
		updateErr := r.Update(ctx, &cluster)
		if updateErr != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", updateErr)
		}

		// The update re-triggers reconciliation; continue on the next pass with the finalizer set.
		return ctrl.Result{}, nil
	}

	log.Info("reconciling cluster", "distribution", cluster.Spec.Cluster.Distribution)

	return r.reconcileNormal(ctx, &cluster)
}

// SetupWithManager registers the reconciler with the controller manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	err := ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Cluster{}).
		Named("cluster").
		Complete(r)
	if err != nil {
		return fmt.Errorf("set up cluster controller: %w", err)
	}

	return nil
}

// reconcileNormal ensures the underlying cluster exists and reports status.
func (r *ClusterReconciler) reconcileNormal(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) (ctrl.Result, error) {
	provisioner, err := r.NewProvisioner(ctx, cluster)
	if err != nil {
		return r.fail(ctx, cluster, "ProvisionerError", err)
	}

	exists, err := provisioner.Exists(ctx, cluster.Name)
	if err != nil {
		return r.fail(ctx, cluster, "ExistsCheckFailed", err)
	}

	if !exists {
		r.markProgressing(
			cluster,
			v1alpha1.ClusterPhaseProvisioning,
			"Creating",
			"Creating cluster",
		)

		statusErr := r.updateStatus(ctx, cluster)
		if statusErr != nil {
			return ctrl.Result{}, statusErr
		}

		createErr := r.provisionAndRecord(ctx, cluster, provisioner)
		if createErr != nil {
			return r.fail(ctx, cluster, "CreateFailed", createErr)
		}
	} else {
		driftErr := r.reconcileDrift(ctx, cluster, provisioner)
		if driftErr != nil {
			return r.fail(ctx, cluster, "UpdateFailed", driftErr)
		}
	}

	r.markReady(cluster)

	statusErr := r.updateStatus(ctx, cluster)
	if statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	return ctrl.Result{RequeueAfter: r.readyRequeue()}, nil
}

// reconcileDelete tears down the underlying cluster and removes the finalizer.
func (r *ClusterReconciler) reconcileDelete(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(cluster, FinalizerName) {
		return ctrl.Result{}, nil
	}

	r.markProgressing(cluster, v1alpha1.ClusterPhaseDeleting, "Deleting", "Deleting cluster")
	// Status update is best-effort during deletion; ignore conflicts on a terminating object.
	_ = r.updateStatus(ctx, cluster)

	provisioner, err := r.NewProvisioner(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build provisioner for deletion: %w", err)
	}

	delErr := provisioner.Delete(ctx, cluster.Name)
	if delErr != nil {
		return ctrl.Result{RequeueAfter: r.transitionalRequeue()}, fmt.Errorf(
			"delete cluster: %w",
			delErr,
		)
	}

	controllerutil.RemoveFinalizer(cluster, FinalizerName)

	err = r.Update(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

// fail records a failure on the cluster status and returns a requeue so reconciliation retries.
func (r *ClusterReconciler) fail(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	reason string,
	cause error,
) (ctrl.Result, error) {
	cluster.Status.Phase = v1alpha1.ClusterPhaseFailed
	cluster.Status.ObservedGeneration = cluster.Generation
	now := metav1.Now()
	cluster.Status.LastReconcileTime = &now

	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: cluster.Generation,
		Reason:             reason,
		Message:            cause.Error(),
	})
	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionDegraded,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: cluster.Generation,
		Reason:             reason,
		Message:            cause.Error(),
	})

	statusErr := r.updateStatus(ctx, cluster)
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
	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: cluster.Generation,
		Reason:             reason,
		Message:            message,
	})
}

// markReady records a successful reconciliation.
func (r *ClusterReconciler) markReady(cluster *v1alpha1.Cluster) {
	cluster.Status.Phase = v1alpha1.ClusterPhaseReady
	cluster.Status.ObservedGeneration = cluster.Generation
	now := metav1.Now()
	cluster.Status.LastReconcileTime = &now

	const message = "Cluster is reconciled and ready"

	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: cluster.Generation,
		Reason:             reasonReconciled,
		Message:            message,
	})
	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: cluster.Generation,
		Reason:             reasonReconciled,
		Message:            message,
	})
	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: cluster.Generation,
		Reason:             reasonReconciled,
		Message:            message,
	})
}

// provisionAndRecord creates the underlying cluster and records the applied spec baseline.
func (r *ClusterReconciler) provisionAndRecord(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	provisioner clusterprovisioner.Provisioner,
) error {
	createErr := provisioner.Create(ctx, cluster.Name)
	if createErr != nil {
		return fmt.Errorf("create cluster: %w", createErr)
	}

	return r.recordAppliedSpec(ctx, cluster)
}

// reconcileDrift detects configuration drift against the last-applied spec and applies an
// in-place update when the provisioner supports it. Provisioners that do not implement Updater
// (or clusters without a recorded baseline) are reconciled to a baseline without changes.
func (r *ClusterReconciler) reconcileDrift(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	provisioner clusterprovisioner.Provisioner,
) error {
	updater, ok := provisioner.(clusterprovisioner.Updater)
	if !ok {
		return nil
	}

	oldSpec, hasBaseline := lastAppliedSpec(cluster)
	if !hasBaseline {
		// No baseline yet (e.g. a cluster adopted by the operator): record the current spec.
		return r.recordAppliedSpec(ctx, cluster)
	}

	newSpec := cluster.Spec.Cluster.DeepCopy()

	diff, err := updater.DiffConfig(ctx, cluster.Name, oldSpec, newSpec)
	if err != nil {
		return fmt.Errorf("diff cluster config: %w", err)
	}

	if diff.TotalChanges() == 0 {
		return nil
	}

	r.markProgressing(
		cluster,
		v1alpha1.ClusterPhaseUpdating,
		"Updating",
		"Applying configuration changes",
	)
	_ = r.updateStatus(ctx, cluster)

	_, err = updater.Update(
		ctx,
		cluster.Name,
		oldSpec,
		newSpec,
		clusterupdate.UpdateOptions{Force: true},
	)
	if err != nil {
		return fmt.Errorf("update cluster: %w", err)
	}

	return r.recordAppliedSpec(ctx, cluster)
}

// lastAppliedSpec returns the cluster spec the operator last provisioned, parsed from the
// last-applied annotation. The second return is false when no valid baseline is recorded.
func lastAppliedSpec(cluster *v1alpha1.Cluster) (*v1alpha1.ClusterSpec, bool) {
	raw, ok := cluster.Annotations[LastAppliedSpecAnnotation]
	if !ok || raw == "" {
		return nil, false
	}

	var spec v1alpha1.ClusterSpec

	err := json.Unmarshal([]byte(raw), &spec)
	if err != nil {
		return nil, false
	}

	return &spec, true
}

// recordAppliedSpec stores the current cluster spec as the drift-detection baseline.
func (r *ClusterReconciler) recordAppliedSpec(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) error {
	data, err := json.Marshal(cluster.Spec.Cluster)
	if err != nil {
		return fmt.Errorf("marshal cluster spec: %w", err)
	}

	if cluster.Annotations == nil {
		cluster.Annotations = map[string]string{}
	}

	cluster.Annotations[LastAppliedSpecAnnotation] = string(data)

	updateErr := r.Update(ctx, cluster)
	if updateErr != nil {
		return fmt.Errorf("record last-applied spec: %w", updateErr)
	}

	return nil
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
