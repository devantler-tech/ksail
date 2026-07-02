package gkeprovisioner

import (
	"context"
	"fmt"

	"cloud.google.com/go/container/apiv1/containerpb"
	"github.com/devantler-tech/ksail/v7/pkg/client/gke"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/gcp"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
)

// Provisioner manages Google Kubernetes Engine clusters via the native Go SDK.
//
// Cluster lifecycle operations delegate to pkg/client/gke.Client (which hides
// the SDK's request shapes and long-running-operation polling); Start/Stop
// delegate node-pool scaling to the GCP infrastructure provider. This mirrors
// the EKS provisioner, but through the Go SDK instead of a shelled-out binary.
type Provisioner struct {
	// name is the cluster name derived from the ksail.yaml.
	name string
	// project is the Google Cloud project ID every GKE call is scoped to.
	project string
	// location is the GKE location (zone or region). Empty means "not
	// pinned": reads resolve the cluster's own location via the
	// all-locations list, while Create — which has nothing to resolve from —
	// requires a concrete value.
	location string
	// clusterSpec is the declarative cluster specification submitted by
	// Create. Required for Create; optional for inspection-only use.
	clusterSpec *containerpb.Cluster
	// client is the GKE SDK wrapper.
	client *gke.Client
	// infraProvider is the GCP provider used for Start/Stop semantics
	// (node-pool scale). Optional: if nil, Start/Stop return an error.
	infraProvider provider.Provider
}

// NewProvisioner builds a Provisioner. The GKE client must be non-nil and the
// project non-empty; clusterSpec is required for Create but optional for
// inspection-only use.
func NewProvisioner(
	name, project, location string,
	clusterSpec *containerpb.Cluster,
	client *gke.Client,
	infraProvider provider.Provider,
) (*Provisioner, error) {
	if client == nil {
		return nil, ErrClientRequired
	}

	if project == "" {
		return nil, ErrProjectRequired
	}

	return &Provisioner{
		name:          name,
		project:       project,
		location:      location,
		clusterSpec:   clusterSpec,
		client:        client,
		infraProvider: infraProvider,
	}, nil
}

// SetProvider sets the infrastructure provider used by Start/Stop.
func (p *Provisioner) SetProvider(prov provider.Provider) {
	p.infraProvider = prov
}

// Create provisions a new GKE cluster from the declarative cluster spec and
// blocks until the create operation completes.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	_ = name // name is encoded in the cluster spec; the CLI name flag is ignored.

	if p.clusterSpec == nil {
		return ErrClusterSpecRequired
	}

	if p.location == "" || p.location == gcp.AllLocations {
		return ErrLocationRequired
	}

	err := p.client.CreateCluster(ctx, p.project, p.location, p.clusterSpec)
	if err != nil {
		return fmt.Errorf("gke create cluster: %w", err)
	}

	return nil
}

// Delete tears down the GKE cluster and blocks until the delete operation
// completes. When no location is pinned, the cluster's own location is
// resolved via the all-locations list first.
func (p *Provisioner) Delete(ctx context.Context, name string) error {
	target := p.resolveName(name)

	location, err := p.resolveLocation(ctx, target)
	if err != nil {
		return err
	}

	err = p.client.DeleteCluster(ctx, p.project, location, target)
	if err != nil {
		return fmt.Errorf("gke delete cluster: %w", err)
	}

	return nil
}

// Start resumes a GKE cluster by scaling every node pool back to its
// configured initial size. GKE control planes are Google-managed and never
// stop, so "start" = "scale the pools back in".
func (p *Provisioner) Start(ctx context.Context, name string) error {
	if p.infraProvider == nil {
		return fmt.Errorf("%w: start requires a GCP provider", clustererr.ErrUnsupportedProvider)
	}

	target := p.resolveName(name)

	err := p.infraProvider.StartNodes(ctx, target)
	if err != nil {
		return fmt.Errorf("start nodes: %w", err)
	}

	return nil
}

// Stop scales every node pool to zero nodes. The GKE control plane continues
// to run (and continues to bill) because Google does not expose a stop
// operation for the managed control plane.
func (p *Provisioner) Stop(ctx context.Context, name string) error {
	if p.infraProvider == nil {
		return fmt.Errorf("%w: stop requires a GCP provider", clustererr.ErrUnsupportedProvider)
	}

	target := p.resolveName(name)

	err := p.infraProvider.StopNodes(ctx, target)
	if err != nil {
		return fmt.Errorf("stop nodes: %w", err)
	}

	return nil
}

// List returns the names of every GKE cluster in the configured location, or
// across all locations in the project when no location is pinned.
func (p *Provisioner) List(ctx context.Context) ([]string, error) {
	clusters, err := p.client.ListClusters(ctx, p.project, p.listLocation())
	if err != nil {
		return nil, fmt.Errorf("gke list clusters: %w", err)
	}

	names := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		names = append(names, cluster.GetName())
	}

	return names, nil
}

// Exists reports whether a cluster with the given name (or the provisioner
// default) exists. Implemented via ListClusters + membership check because
// GetCluster reports a missing cluster as a gRPC NotFound error, which is
// harder to classify reliably than an empty list result — the same trade the
// EKS provisioner makes.
func (p *Provisioner) Exists(ctx context.Context, name string) (bool, error) {
	target := p.resolveName(name)
	if target == "" {
		return false, nil
	}

	clusters, err := p.client.ListClusters(ctx, p.project, p.listLocation())
	if err != nil {
		return false, fmt.Errorf("gke list clusters: %w", err)
	}

	for _, cluster := range clusters {
		if cluster.GetName() == target {
			return true, nil
		}
	}

	return false, nil
}

// resolveName returns the caller-supplied name when set, otherwise falls
// back to the provisioner's configured name.
func (p *Provisioner) resolveName(name string) string {
	if name != "" {
		return name
	}

	return p.name
}

// listLocation returns the location used for list-shaped calls: the pinned
// location when set, otherwise the GKE API's all-locations wildcard.
func (p *Provisioner) listLocation() string {
	if p.location == "" {
		return gcp.AllLocations
	}

	return p.location
}

// resolveLocation returns the pinned location when set; otherwise it resolves
// the named cluster's own location via the all-locations list, mirroring the
// GCP provider's resolution for cluster-scoped calls.
func (p *Provisioner) resolveLocation(ctx context.Context, clusterName string) (string, error) {
	if p.location != "" && p.location != gcp.AllLocations {
		return p.location, nil
	}

	clusters, err := p.client.ListClusters(ctx, p.project, gcp.AllLocations)
	if err != nil {
		return "", fmt.Errorf("resolve cluster location: %w", err)
	}

	for _, cluster := range clusters {
		if cluster.GetName() == clusterName {
			return cluster.GetLocation(), nil
		}
	}

	return "", fmt.Errorf("%w: %s", ErrClusterNotFound, clusterName)
}
