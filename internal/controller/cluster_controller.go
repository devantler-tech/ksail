// Package controller contains the controller-runtime reconcilers for the KSail operator.
package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"k8s.io/apimachinery/pkg/api/equality"
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

// maxProvisionedNameLen bounds ProvisionedName so downstream provisioners that derive a Kubernetes
// namespace from it (e.g. vcluster's "vcluster-<name>") stay within the 63-char DNS-1123 label
// limit: 63 - len("vcluster-") = 54.
const maxProvisionedNameLen = 54

// ProvisionedName returns the name of the underlying cluster the operator provisions for a
// Cluster resource. It is qualified with the resource namespace so two Cluster resources with the
// same name in different namespaces never collide on the same underlying cluster (and its
// kubeconfig). The Cluster CRD is namespaced, so name alone is not unique across the hub cluster.
// Long namespace/name combinations are deterministically truncated with a hash suffix so the
// result always fits the DNS-1123 label limit expected by downstream provisioners.
func ProvisionedName(cluster *v1alpha1.Cluster) string {
	name := cluster.Name
	if cluster.Namespace != "" {
		name = cluster.Namespace + "-" + cluster.Name
	}

	if len(name) <= maxProvisionedNameLen {
		return name
	}

	sum := sha256.Sum256([]byte(name))
	suffix := hex.EncodeToString(sum[:])[:8]
	prefix := strings.TrimRight(name[:maxProvisionedNameLen-len(suffix)-1], "-")

	return prefix + "-" + suffix
}

// ProvisionerBuilder returns a distribution provisioner for the given Cluster. The operator
// supplies a builder backed by the existing provisioner factory (forcing the Kubernetes
// provider so clusters are provisioned in-cluster); tests supply a fake.
type ProvisionerBuilder func(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, error)

// ObservedStatus is the runtime state of a provisioned cluster, gathered best-effort by a
// StatusObserver. Zero-valued fields are treated as "not observed" and leave the existing status
// untouched, so a transient observation failure never erases previously reported data.
type ObservedStatus struct {
	// Endpoint is the stable API server URL of the provisioned cluster.
	Endpoint string
	// KubeconfigSecret references the Secret holding the child cluster's kubeconfig.
	KubeconfigSecret *v1alpha1.SecretReference
	// NodesReady and NodesTotal are populated only when NodesObserved is true.
	NodesReady    int32
	NodesTotal    int32
	NodesObserved bool
}

// StatusObserver gathers runtime status (endpoint, kubeconfig, node readiness) for a provisioned
// cluster. It is optional and distribution-specific: the operator injects an implementation, while
// tests may leave it nil. It is invoked best-effort, so it must tolerate a not-yet-ready cluster.
// The reader is uncached (it reads a single Secret directly) to avoid caching every cluster Secret.
type StatusObserver func(
	ctx context.Context,
	hub client.Reader,
	cluster *v1alpha1.Cluster,
) (ObservedStatus, error)

// ClusterReconciler reconciles a Cluster object towards its desired state.
type ClusterReconciler struct {
	client.Client

	Scheme *runtime.Scheme

	// NewProvisioner builds the provisioner used to create/delete the underlying cluster.
	NewProvisioner ProvisionerBuilder

	// ObserveStatus gathers runtime status (endpoint, node readiness) best-effort. Optional; nil
	// disables runtime status reporting (endpoint/nodes stay empty).
	ObserveStatus StatusObserver

	// APIReader is an uncached reader used by ObserveStatus to read a single kubeconfig Secret
	// without forcing the manager cache to watch every Secret. Falls back to the cached client.
	APIReader client.Reader

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
	before := cluster.Status.DeepCopy()

	provisioner, err := r.NewProvisioner(ctx, cluster)
	if err != nil {
		return r.fail(ctx, cluster, "ProvisionerError", err)
	}

	exists, err := provisioner.Exists(ctx, ProvisionedName(cluster))
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

		statusErr := r.updateStatusIfChanged(ctx, cluster, before)
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

	// Gather runtime status (endpoint, kubeconfig, node readiness) best-effort: a failure here must
	// not fail an otherwise-successful reconcile, and partial results are still applied.
	r.observeStatus(ctx, cluster)

	r.markReady(cluster)

	statusErr := r.updateStatusIfChanged(ctx, cluster, before)
	if statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	return ctrl.Result{RequeueAfter: r.readyRequeue()}, nil
}

// observeStatus populates runtime status fields (endpoint, kubeconfig ref, node counts) from the
// optional StatusObserver. It is best-effort: observation errors are logged and partial results
// applied, since a not-yet-reachable child cluster is expected shortly after provisioning.
func (r *ClusterReconciler) observeStatus(ctx context.Context, cluster *v1alpha1.Cluster) {
	if r.ObserveStatus == nil {
		return
	}

	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}

	observed, err := r.ObserveStatus(ctx, reader, cluster)
	if err != nil {
		logf.FromContext(ctx).
			Info("observe child cluster status (best-effort)", "error", err.Error())
	}

	if observed.Endpoint != "" {
		cluster.Status.Endpoint = observed.Endpoint
	}

	if observed.KubeconfigSecret != nil {
		cluster.Status.KubeconfigSecretRef = observed.KubeconfigSecret
	}

	if observed.NodesObserved {
		cluster.Status.NodesReady = observed.NodesReady
		cluster.Status.NodesTotal = observed.NodesTotal
	}
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

	delErr := provisioner.Delete(ctx, ProvisionedName(cluster))
	if delErr != nil {
		// Return only the error: controller-runtime ignores Result when err != nil and applies
		// its rate-limited backoff, so a RequeueAfter here would have no effect.
		return ctrl.Result{}, fmt.Errorf("delete cluster: %w", delErr)
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
	before := cluster.Status.DeepCopy()

	cluster.Status.Phase = v1alpha1.ClusterPhaseFailed
	cluster.Status.ObservedGeneration = cluster.Generation

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
	// Clear Progressing so a previously-Progressing cluster does not stay Progressing while Failed.
	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: cluster.Generation,
		Reason:             reason,
		Message:            cause.Error(),
	})

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
	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: cluster.Generation,
		Reason:             reason,
		Message:            message,
	})
}

// markReady records a successful reconciliation. LastReconcileTime is set by
// updateStatusIfChanged only when the status actually changes, to avoid status-write churn.
func (r *ClusterReconciler) markReady(cluster *v1alpha1.Cluster) {
	cluster.Status.Phase = v1alpha1.ClusterPhaseReady
	cluster.Status.ObservedGeneration = cluster.Generation

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
	createErr := provisioner.Create(ctx, ProvisionedName(cluster))
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

	diff, err := updater.DiffConfig(ctx, ProvisionedName(cluster), oldSpec, newSpec)
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
		ProvisionedName(cluster),
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
