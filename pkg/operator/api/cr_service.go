package api

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// defaultNamespace is used when a create request does not specify a namespace.
	defaultNamespace = "default"
	// lastAppliedSpecAnnotation is the operator-managed drift baseline annotation
	// (controller.LastAppliedSpecAnnotation); the API strips it from client input.
	lastAppliedSpecAnnotation = "ksail.io/last-applied-spec"
)

// crClusterService backs the REST API with the controller-runtime client, CRUDing Cluster custom
// resources. This is the operator's backend; the API runs with the operator's RBAC.
type crClusterService struct {
	client client.Client
}

// NewCRClusterService returns a ClusterService backed by the controller-runtime client.
func NewCRClusterService(kubeClient client.Client) ClusterService {
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

// ensureNamespace creates the namespace on demand, labelled operator-managed so the reconciler can
// clean it up on cluster deletion. An existing namespace (operator-managed or not) is left as-is.
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
// and the operator-managed last-applied-spec annotation so the API cannot be used to interfere with
// reconciliation or drift detection.
func sanitizeForWrite(cluster *v1alpha1.Cluster) *v1alpha1.Cluster {
	out := &v1alpha1.Cluster{}
	out.Name = cluster.Name
	out.Namespace = cluster.Namespace
	out.Labels = cluster.Labels
	out.Spec = cluster.Spec

	if len(cluster.Annotations) > 0 {
		annotations := make(map[string]string, len(cluster.Annotations))

		for key, value := range cluster.Annotations {
			if key == lastAppliedSpecAnnotation {
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
