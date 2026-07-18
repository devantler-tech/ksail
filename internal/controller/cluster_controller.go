// Package controller contains the controller-runtime reconcilers for the KSail operator.
package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
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
// cluster before the custom resource is removed from the API server. It re-exports the single
// definition in pkg/apis/cluster/v1alpha1 (shared with the REST API) so the wire value cannot drift.
const FinalizerName = v1alpha1.FinalizerName

// LastAppliedSpecAnnotation stores the JSON of the cluster spec the operator last provisioned,
// used as the baseline for drift detection on subsequent reconciles. It re-exports the single
// definition in pkg/apis/cluster/v1alpha1 (shared with the REST API, which strips it from client
// input) so the controller and the API can never silently desync on a rename.
const LastAppliedSpecAnnotation = v1alpha1.LastAppliedSpecAnnotation

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

// ComponentInstaller installs the cluster's components (CNI/CSI/metrics-server/cert-manager/
// load-balancer/policy-engine/GitOps) into the provisioned child cluster. It receives the
// provisioner so it can obtain child access via the provisioner's optional Connector capability.
// Optional; nil disables component installation. The reconciler invokes it with gating — only when
// components are not already reconciled for the current generation — and treats it best-effort.
//
// The returned applied is false when installation was skipped because it is not supported for this
// cluster (e.g. the provisioner exposes no operator-reachable kubeconfig); the reconciler then
// reports ComponentsReady=Unknown rather than a misleading True.
//
// The returned components carry the per-component install outcome (one entry per declared component);
// the reconciler records them in ClusterStatus.Components so a UI can surface per-component health.
type ComponentInstaller func(
	ctx context.Context,
	provisioner clusterprovisioner.Provisioner,
	cluster *v1alpha1.Cluster,
) (applied bool, components []v1alpha1.ComponentStatus, err error)

// ClusterReconciler reconciles a Cluster object towards its desired state.
type ClusterReconciler struct {
	client.Client

	Scheme *runtime.Scheme

	// NewProvisioner builds the provisioner used to create/delete the underlying cluster.
	NewProvisioner ProvisionerBuilder

	// ObserveStatus gathers runtime status (endpoint, node readiness) best-effort. Optional; nil
	// disables runtime status reporting (endpoint/nodes stay empty).
	ObserveStatus StatusObserver

	// ObserveHostStatus gathers runtime status for the self-registered host cluster (the cluster the
	// operator runs on) through the operator's own credentials. Optional; nil disables runtime status
	// reporting for the host cluster.
	ObserveHostStatus StatusObserver

	// HostClusterNamespace is the only namespace in which a host-labelled Cluster is trusted as the
	// operator's self-registration. Empty disables the host-cluster fast path.
	HostClusterNamespace string

	// InstallComponents installs the cluster's components into the provisioned child cluster.
	// Optional; nil disables component installation.
	InstallComponents ComponentInstaller

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

	// The host cluster (the operator's self-registration of the cluster it runs on) is never
	// provisioned or torn down, so it gets no finalizer and a status-only reconcile.
	if cluster.IsHostClusterRegistration(r.HostClusterNamespace) {
		log.Info("reconciling host cluster")

		return r.reconcileHost(ctx, &cluster)
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

	// Install the spec's components into the child cluster (best-effort, gated by generation).
	componentsOK := r.reconcileComponents(ctx, provisioner, cluster)

	// Surface any CLI-only spec fields the operator does not reconcile (informational only).
	r.setIgnoredFieldsCondition(cluster)

	r.markReady(cluster)

	statusErr := r.updateStatusIfChanged(ctx, cluster, before)
	if statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	// Retry sooner while components are still being installed or have failed.
	if !componentsOK {
		return ctrl.Result{RequeueAfter: r.transitionalRequeue()}, nil
	}

	return ctrl.Result{RequeueAfter: r.readyRequeue()}, nil
}

// reconcileHost reconciles the self-registered host cluster. The underlying cluster — the one the
// operator runs on — already exists and is owned by whoever provisioned it, so there is nothing to
// create, update, or delete: reconciliation only observes runtime status (node readiness, endpoint)
// through the operator's own credentials and reports it. Component installation is intentionally
// skipped; the host cluster's components are not the operator's to manage.
func (r *ClusterReconciler) reconcileHost(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) (ctrl.Result, error) {
	before := cluster.Status.DeepCopy()

	r.observeStatusWith(ctx, r.ObserveHostStatus, cluster)

	setCondition(
		cluster,
		v1alpha1.ConditionComponentsReady,
		metav1.ConditionUnknown,
		"HostCluster",
		"components on the host cluster are not managed by the operator",
	)

	// The host cluster carries an empty spec, so this reports None — but keeping it on the host path
	// too means the condition is always present and consistent across reconcile branches.
	r.setIgnoredFieldsCondition(cluster)

	r.markReady(cluster)

	statusErr := r.updateStatusIfChanged(ctx, cluster, before)
	if statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	return ctrl.Result{RequeueAfter: r.readyRequeue()}, nil
}

// reconcileComponents installs the cluster's components when they are not already reconciled for the
// current generation, recording the outcome in the ComponentsReady condition. Best-effort: failures
// are reported via the condition (not the reconcile error) and return false so the reconcile
// requeues sooner. Returns true when components are up to date or there is nothing to install.
func (r *ClusterReconciler) reconcileComponents(
	ctx context.Context,
	provisioner clusterprovisioner.Provisioner,
	cluster *v1alpha1.Cluster,
) bool {
	if r.InstallComponents == nil || componentsUpToDate(cluster) {
		return true
	}

	applied, components, err := r.InstallComponents(ctx, provisioner, cluster)
	cluster.Status.Components = components

	if err != nil {
		logf.FromContext(ctx).Info("install components (best-effort)", "error", err.Error())
		setCondition(
			cluster,
			v1alpha1.ConditionComponentsReady,
			metav1.ConditionFalse,
			"ComponentsFailed",
			err.Error(),
		)

		return false
	}

	if !applied {
		// Component installation is not supported for this cluster (e.g. the provisioner exposes no
		// operator-reachable kubeconfig). Report Unknown rather than a misleading Ready=True.
		setCondition(
			cluster,
			v1alpha1.ConditionComponentsReady,
			metav1.ConditionUnknown,
			"NotSupported",
			"component installation is not supported for this distribution/provider",
		)

		return true
	}

	// Record the spec we just reconciled components for, so a later spec change that drops a component
	// (e.g. policyEngine Kyverno→None) is detected next reconcile and the component uninstalled. A
	// write failure keeps ComponentsReady False so it retries — the baseline must exist before the
	// condition reads True, or a removal could go undetected.
	recordErr := r.recordAppliedComponents(ctx, cluster)
	if recordErr != nil {
		logf.FromContext(ctx).
			Info("record components baseline (best-effort)", "error", recordErr.Error())
		setCondition(
			cluster,
			v1alpha1.ConditionComponentsReady,
			metav1.ConditionFalse,
			"BaselineRecordFailed",
			recordErr.Error(),
		)

		return false
	}

	setCondition(
		cluster,
		v1alpha1.ConditionComponentsReady,
		metav1.ConditionTrue,
		reasonReconciled,
		"Components installed and reconciled",
	)

	return true
}

// componentsUpToDate reports whether component reconciliation has settled for the cluster's current
// generation (so a spec change re-triggers installation). A condition observed at the current
// generation is settled unless it is False (a failure), which keeps retrying; True (installed) and
// Unknown (not supported) are terminal for that generation.
func componentsUpToDate(cluster *v1alpha1.Cluster) bool {
	condition := apimeta.FindStatusCondition(
		cluster.Status.Conditions,
		v1alpha1.ConditionComponentsReady,
	)

	return condition != nil &&
		condition.ObservedGeneration == cluster.Generation &&
		condition.Status != metav1.ConditionFalse
}

// observeStatus populates runtime status fields (endpoint, kubeconfig ref, node counts) from the
// optional StatusObserver. It is best-effort: observation errors are logged and partial results
// applied, since a not-yet-reachable child cluster is expected shortly after provisioning.
func (r *ClusterReconciler) observeStatus(ctx context.Context, cluster *v1alpha1.Cluster) {
	r.observeStatusWith(ctx, r.ObserveStatus, cluster)
}

// observeStatusWith applies the given observer's results to the cluster status. Shared by the
// child-cluster path (ObserveStatus) and the host-cluster path (ObserveHostStatus); a nil observer
// disables observation.
func (r *ClusterReconciler) observeStatusWith(
	ctx context.Context,
	observer StatusObserver,
	cluster *v1alpha1.Cluster,
) {
	if observer == nil {
		return
	}

	observed, err := observer(ctx, r.reader(), cluster)
	if err != nil {
		logf.FromContext(ctx).
			Info("observe cluster status (best-effort)", "error", err.Error())
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

	// Deleting the well-known host registration must never destroy anything: the underlying cluster is
	// the one the operator runs on. If the registration somehow carries a finalizer, remove it without
	// invoking the provisioner.
	if cluster.IsHostClusterRegistration(r.HostClusterNamespace) {
		controllerutil.RemoveFinalizer(cluster, FinalizerName)

		err := r.Update(ctx, cluster)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
		}

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

	// Best-effort: remove the namespace if the operator created it and it now holds nothing else.
	r.cleanupNamespace(ctx, cluster)

	return ctrl.Result{}, nil
}

// reader returns the uncached API reader when available (avoids caching every namespace/workload),
// falling back to the cached client.
func (r *ClusterReconciler) reader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}

	return r.Client
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
