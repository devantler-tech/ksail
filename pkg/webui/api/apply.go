package api

import "context"

// ApplyResult reports the outcome of applying one manifest document.
type ApplyResult struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	// Status is "applied" when server-side apply succeeded, or "error".
	Status string `json:"status"`
	// Error carries the failure detail when Status is "error".
	Error string `json:"error,omitempty"`
}

// ApplyService is an optional interface a ClusterService may implement to server-side-apply raw
// Kubernetes manifests (multi-document YAML) to a target cluster, with an optional dry-run for
// validation. When the serving ClusterService implements it, the server registers
// POST /api/v1/clusters/{ns}/{name}/apply (a mutating verb, so the read-only guard rejects it in
// read-only mode) and advertises capabilities.applyManifests=true. Each document is applied
// independently and reported separately so a single bad document does not fail the whole batch.
type ApplyService interface {
	ApplyManifests(
		ctx context.Context,
		namespace, name string,
		manifests []byte,
		dryRun bool,
	) ([]ApplyResult, error)
}
