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
// which drives the provider/provisioner lifecycle for `ksail ui`.
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

// Capabilities reports which optional operations a backend supports, so the SPA can hide affordances
// a backend cannot fulfill instead of offering an action that fails. It is served on
// /api/v1/config under "capabilities". New capability flags are added here as the UI surface grows
// (e.g. workload reads, log streaming, exec), and each backend reports the subset it implements.
type Capabilities struct {
	// ClusterUpdate reports whether the backend can apply spec changes to an existing cluster
	// (PUT /api/v1/clusters/{namespace}/{name}). The operator patches the Cluster custom resource and
	// supports it; the local CLI backend manages cluster configuration via files and does not, so the
	// SPA hides the edit affordance there rather than offering an action that returns 501.
	ClusterUpdate bool `json:"clusterUpdate"`
	// WorkloadRead reports whether the backend can read live Kubernetes resources from a target
	// cluster (the read-only workload browser). It is true exactly when the serving ClusterService
	// implements ResourceService; the SPA shows the Resources view only then. Derived from the
	// interface in handleConfig rather than reported via CapabilityReporter, so it cannot drift from
	// whether the endpoints are actually registered.
	WorkloadRead bool `json:"workloadRead"`
	// WorkloadWrite reports whether the backend exposes the safe write actions (scale, rollout
	// restart, delete) on browsable resources — true exactly when the serving ClusterService
	// implements ResourceWriter. The SPA still combines it with !readOnly before showing the actions.
	WorkloadWrite bool `json:"workloadWrite"`
}

// CapabilityReporter is an optional interface a ClusterService may implement to advertise which
// operations it supports. A ClusterService that does not implement it is assumed to support the full
// surface (see fullCapabilities) — the operator's controller-runtime backend relies on this default.
type CapabilityReporter interface {
	Capabilities() Capabilities
}

// fullCapabilities is the capability set assumed for a ClusterService that does not implement
// CapabilityReporter: every operation is supported.
func fullCapabilities() Capabilities {
	return Capabilities{ClusterUpdate: true}
}

// serviceCapabilities returns the capabilities a ClusterService advertises, defaulting to the full
// surface when it does not implement CapabilityReporter.
func serviceCapabilities(service ClusterService) Capabilities {
	if reporter, ok := service.(CapabilityReporter); ok {
		return reporter.Capabilities()
	}

	return fullCapabilities()
}
