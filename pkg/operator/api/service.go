package api

import (
	"context"
	"errors"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// Sentinel errors a ClusterService implementation may return. clientErrorStatus maps them to HTTP
// status codes so backends that do not surface Kubernetes apierrors (e.g. the local CLI backend)
// can still drive the correct responses.
var (
	// ErrNotFound indicates the requested cluster does not exist (HTTP 404).
	ErrNotFound = errors.New("cluster not found")
	// ErrAlreadyExists indicates a cluster with the requested name already exists (HTTP 409).
	ErrAlreadyExists = errors.New("cluster already exists")
	// ErrInvalid indicates the client supplied an invalid cluster definition (HTTP 422).
	ErrInvalid = errors.New("invalid cluster")
	// ErrNotSupported indicates the backend does not support the requested operation (HTTP 501).
	ErrNotSupported = errors.New("operation not supported")
)

// ClusterService is the backend the REST handlers delegate to. It is expressed in the
// Kubernetes-shaped wire types the web UI already consumes (v1alpha1.Cluster / v1alpha1.ClusterList)
// so a single HTTP layer can be served by two implementations: the operator's controller-runtime
// backend (crClusterService) which CRUDs Cluster custom resources, and the CLI's local backend
// which drives the provider/provisioner lifecycle for `ksail cluster ui`.
//
// Implementations may return the sentinel errors below; clientErrorStatus maps them (and Kubernetes
// apierrors) to HTTP status codes. Returning any other error yields HTTP 500.
type ClusterService interface {
	// List returns all clusters. Items must be non-nil (empty slice, not nil) so the JSON encodes
	// as [] rather than null, matching Kubernetes list semantics.
	List(ctx context.Context) (*v1alpha1.ClusterList, error)
	// Get returns a single cluster, or a not-found error.
	Get(ctx context.Context, namespace, name string) (*v1alpha1.Cluster, error)
	// Create provisions a new cluster from client-supplied input and returns the created object.
	Create(ctx context.Context, cluster *v1alpha1.Cluster) (*v1alpha1.Cluster, error)
	// Update applies the client-supplied spec to an existing cluster and returns the updated object.
	Update(
		ctx context.Context,
		namespace, name string,
		cluster *v1alpha1.Cluster,
	) (*v1alpha1.Cluster, error)
	// Delete removes a cluster.
	Delete(ctx context.Context, namespace, name string) error
}
