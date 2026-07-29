package operator

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// defaultNamespace is used when a create request does not specify a namespace.
const defaultNamespace = "default"

// crClusterService backs the REST API with the controller-runtime client, CRUDing Cluster custom
// resources. This is the operator's backend; the API runs with the operator's RBAC.
type crClusterService struct {
	client client.Client
}

// The operator backend supports in-place cluster update (it patches the Cluster CR), so it implements
// the optional ClusterUpdater interface — that is what advertises capabilities.clusterUpdate=true. It
// also implements ComponentInstaller (its reconciler installs the declared components when a
// provisioner exposes a Connector), so the create form offers the component selectors.
var (
	_ api.ClusterUpdater     = (*crClusterService)(nil)
	_ api.ComponentInstaller = (*crClusterService)(nil)
)

// InstallsComponents reports true: the operator's reconciler installs the cluster components declared
// in the spec (CNI, CSI, metrics-server, …) when the provisioner exposes child access, recording the
// outcome in the ComponentsReady condition. The capability lets the create form offer the component
// selectors knowing they are honored.
func (s *crClusterService) InstallsComponents() bool {
	return true
}

// NewCRClusterService returns a ClusterService backed by the controller-runtime client.
func NewCRClusterService(kubeClient client.Client) api.ClusterService {
	return &crClusterService{client: kubeClient}
}

func (s *crClusterService) List(ctx context.Context) (*v1alpha1.ClusterList, error) {
	var list v1alpha1.ClusterList

	err := s.client.List(ctx, &list)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}

	// Emit an empty array rather than null for items when there are no clusters,
	// matching Kubernetes list semantics so clients don't have to special-case null.
	if list.Items == nil {
		list.Items = []v1alpha1.Cluster{}
	}

	return &list, nil
}

func (s *crClusterService) Get(
	ctx context.Context,
	namespace, name string,
) (*v1alpha1.Cluster, error) {
	var cluster v1alpha1.Cluster

	key := types.NamespacedName{Namespace: namespace, Name: name}

	err := s.client.Get(ctx, key, &cluster)
	if err != nil {
		return nil, fmt.Errorf("get cluster: %w", err)
	}

	return &cluster, nil
}

func (s *crClusterService) Create(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) (*v1alpha1.Cluster, error) {
	// The host-cluster label is reserved for the operator's self-registration; a client-created
	// cluster carrying it would just alias the hub and bypass the reconciler.
	if cluster.IsHostCluster() {
		return nil, fmt.Errorf(
			"%w: the %s label is reserved for the operator",
			api.ErrHostClusterProtected, v1alpha1.HostClusterLabel,
		)
	}

	sanitized := sanitizeForWrite(cluster)
	if sanitized.Namespace == "" {
		sanitized.Namespace = defaultNamespace
	}

	// A Cluster cannot be created in a namespace that does not exist, so create it on demand. The
	// namespace is labelled operator-managed so the reconciler can clean it up on deletion.
	nsErr := s.ensureNamespace(ctx, sanitized.Namespace)
	if nsErr != nil {
		return nil, nsErr
	}

	err := s.client.Create(ctx, sanitized)
	if err != nil {
		return nil, fmt.Errorf("create cluster: %w", err)
	}

	return sanitized, nil
}

func (s *crClusterService) Update(
	ctx context.Context,
	namespace, name string,
	cluster *v1alpha1.Cluster,
) (*v1alpha1.Cluster, error) {
	key := types.NamespacedName{Namespace: namespace, Name: name}

	// Fetch the existing object so the update carries the current resourceVersion and preserves
	// server- and operator-managed fields (status, finalizers, operator annotations). Only the
	// client-mutable spec is applied.
	var existing v1alpha1.Cluster

	err := s.client.Get(ctx, key, &existing)
	if err != nil {
		return nil, fmt.Errorf("get cluster: %w", err)
	}

	// The host cluster's spec is not reconciled (the operator does not manage the hub's lifecycle),
	// so spec edits through the API would only mislead.
	if existing.IsHostCluster() {
		return nil, api.ErrHostClusterProtected
	}

	existing.Spec = cluster.Spec

	updateErr := s.client.Update(ctx, &existing)
	if updateErr != nil {
		return nil, fmt.Errorf("update cluster: %w", updateErr)
	}

	return &existing, nil
}

func (s *crClusterService) Delete(ctx context.Context, namespace, name string) error {
	cluster := &v1alpha1.Cluster{}
	cluster.Namespace = namespace
	cluster.Name = name

	// Deleting the host registration through the API is blocked (like Rancher's "local" cluster):
	// "delete" reads as destroying the cluster, and the hub hosting the operator must never be a
	// one-click casualty. kubectl remains the escape hatch — CR deletion only deregisters.
	var existing v1alpha1.Cluster

	getErr := s.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &existing)
	if getErr == nil && existing.IsHostCluster() {
		return api.ErrHostClusterProtected
	}

	err := s.client.Delete(ctx, cluster)
	if err != nil {
		return fmt.Errorf("delete cluster: %w", err)
	}

	return nil
}

// ensureNamespace creates the namespace on demand, labelled operator-managed so the reconciler can
// clean it up on cluster deletion. An existing namespace (operator-managed or not) is left as-is.
func (s *crClusterService) ensureNamespace(ctx context.Context, name string) error {
	var existing corev1.Namespace

	getErr := s.client.Get(ctx, types.NamespacedName{Name: name}, &existing)
	if getErr == nil {
		return nil
	}

	if !apierrors.IsNotFound(getErr) {
		return fmt.Errorf("check namespace %q: %w", name, getErr)
	}

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{v1alpha1.ManagedNamespaceLabel: "true"},
		},
	}

	createErr := s.client.Create(ctx, namespace)
	// Tolerate a concurrent creation (another request created it first).
	if createErr != nil && !apierrors.IsAlreadyExists(createErr) {
		return fmt.Errorf("create namespace %q: %w", name, createErr)
	}

	return nil
}

// sanitizeForWrite returns a copy of a client-supplied Cluster containing only the fields a caller
// is allowed to set (name, namespace, labels, spec). It drops status, finalizers, resourceVersion,
// and the operator-managed last-applied baseline annotations so the API cannot be used to interfere
// with reconciliation, drift detection, or component-removal detection.
func sanitizeForWrite(cluster *v1alpha1.Cluster) *v1alpha1.Cluster {
	out := &v1alpha1.Cluster{}
	out.Name = cluster.Name
	out.Namespace = cluster.Namespace
	out.Labels = cluster.Labels
	out.Spec = cluster.Spec

	if len(cluster.Annotations) > 0 {
		annotations := make(map[string]string, len(cluster.Annotations))

		for key, value := range cluster.Annotations {
			if key == v1alpha1.LastAppliedSpecAnnotation ||
				key == v1alpha1.LastAppliedComponentsAnnotation ||
				key == v1alpha1.AWSLoadBalancerControllerReleaseIdentityAnnotation {
				continue
			}

			annotations[key] = value
		}

		if len(annotations) > 0 {
			out.Annotations = annotations
		}
	}

	return out
}
