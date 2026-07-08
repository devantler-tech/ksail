package gcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cloud.google.com/go/container/apiv1/containerpb"
	"github.com/devantler-tech/ksail/v7/pkg/client/gke"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

// AllLocations is the GKE API's location wildcard, listing clusters across
// every location in a project.
const AllLocations = "-"

// nodeRoleWorker is the role every GKE node pool maps to: the control plane
// is Google-managed and never surfaces as a pool.
const nodeRoleWorker = "worker"

// Provider implements provider.Provider for Google Kubernetes Engine.
type Provider struct {
	client   *gke.Client
	project  string
	location string
}

// NewProvider returns a Provider for the given project and location using the
// given GKE client. Pass location="" to operate across all locations where
// the API allows it (cluster listing); cluster-scoped calls then resolve the
// cluster's own location via ListClusters. A nil client returns
// ErrClientRequired and an empty project ErrProjectRequired so callers fail
// fast rather than at the first API call.
func NewProvider(client *gke.Client, project string, location string) (*Provider, error) {
	if client == nil {
		return nil, ErrClientRequired
	}

	if project == "" {
		return nil, ErrProjectRequired
	}

	return &Provider{client: client, project: project, location: location}, nil
}

// StartNodes resizes every node pool of the cluster to its configured
// initial node count (floored at one node).
//
// GKE does not expose a pool's live size on the NodePool message, so unlike
// EKS there is no "already running, leave alone" check — the resize is
// re-asserted for every pool, which the API treats as a no-op when the pool
// is already at that size. The provisioner — which has the ksail.yaml spec —
// is responsible for restoring an exact desired size when needed.
func (p *Provider) StartNodes(ctx context.Context, clusterName string) error {
	pools, location, err := p.nodePoolsForScale(ctx, clusterName)
	if err != nil {
		return err
	}

	for _, pool := range pools {
		target := max(pool.GetInitialNodeCount(), 1)

		err = p.client.SetNodePoolSize(
			ctx, p.project, location, clusterName, pool.GetName(), target,
		)
		if err != nil {
			return fmt.Errorf("start nodes: resize node pool %s: %w", pool.GetName(), err)
		}
	}

	return nil
}

// StopNodes resizes every node pool of the cluster to zero nodes. The GKE
// control plane keeps running (and billing) but all node costs stop.
func (p *Provider) StopNodes(ctx context.Context, clusterName string) error {
	pools, location, err := p.nodePoolsForScale(ctx, clusterName)
	if err != nil {
		return err
	}

	for _, pool := range pools {
		err = p.client.SetNodePoolSize(
			ctx, p.project, location, clusterName, pool.GetName(), 0,
		)
		if err != nil {
			return fmt.Errorf("stop nodes: resize node pool %s: %w", pool.GetName(), err)
		}
	}

	return nil
}

// ListNodes returns one NodeInfo per node pool of the cluster.
//
// GKE node pools are managed instance groups, not individual machines, so
// KSail collapses the distinction and represents each pool as a single
// NodeInfo whose Name is the pool name — mirroring how the AWS provider
// represents EKS nodegroups.
func (p *Provider) ListNodes(
	ctx context.Context,
	clusterName string,
) ([]provider.NodeInfo, error) {
	cluster, err := p.getCluster(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	return nodePoolInfos(cluster, clusterName), nil
}

// ListAllClusters returns the names of all GKE clusters in the configured
// project and location (all locations when the provider was built with an
// empty location).
func (p *Provider) ListAllClusters(ctx context.Context) ([]string, error) {
	clusters, err := provider.FetchOrTranslate(
		p.client != nil,
		func() ([]*containerpb.Cluster, error) {
			return p.client.ListClusters(ctx, p.project, p.listLocation())
		},
		translateClientErr,
	)
	if err != nil {
		return nil, err //nolint:wrapcheck // FetchOrTranslate already ran the error through translateClientErr
	}

	return provider.NamesFrom(
		clusters,
		func(c *containerpb.Cluster) string { return c.GetName() },
	), nil
}

// NodesExist returns true if the cluster has at least one node pool.
func (p *Provider) NodesExist(ctx context.Context, clusterName string) (bool, error) {
	exists, err := provider.CheckNodesExist(ctx, p, clusterName)
	if err != nil {
		return false, fmt.Errorf("check gke node pools: %w", err)
	}

	return exists, nil
}

// DeleteNodes is a no-op for GKE: deleting the cluster deletes its node
// pools atomically, so pool deletion is owned by the provisioner's cluster
// deletion — mirroring the AWS provider's contract for EKS nodegroups.
func (p *Provider) DeleteNodes(_ context.Context, _ string) error {
	return nil
}

// GetClusterStatus aggregates node-pool statuses into a
// provider.ClusterStatus. Returns provider.ErrClusterNotFound when the
// cluster does not exist. A cluster that exists but has no node pools yields
// a zero-node "stopped" status rather than a nil status, keeping the
// (status, err) contract: status is non-nil whenever err is nil.
func (p *Provider) GetClusterStatus(
	ctx context.Context,
	clusterName string,
) (*provider.ClusterStatus, error) {
	cluster, err := p.getCluster(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	nodes := nodePoolInfos(cluster, clusterName)

	status := provider.BuildClusterStatus(nodes, containerpb.NodePool_RUNNING.String())
	if status == nil {
		status = &provider.ClusterStatus{Phase: provider.PhaseStopped, Nodes: nodes}
	}

	status.Endpoint = cluster.GetEndpoint()

	return status, nil
}

// Project returns the Google Cloud project this provider was configured with.
func (p *Provider) Project() string {
	return p.project
}

// getCluster is the shared cluster fetch that guards against nil clients and
// translates client errors into the provider sentinels.
func (p *Provider) getCluster(
	ctx context.Context,
	clusterName string,
) (*containerpb.Cluster, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	location, err := p.clusterLocation(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	cluster, err := p.client.GetCluster(ctx, p.project, location, clusterName)
	if err != nil {
		return nil, translateClientErr(err)
	}

	return cluster, nil
}

// nodePoolsForScale is the shared prelude for StartNodes and StopNodes that
// fetches the cluster's node pools (and the location they live in) and
// classifies an empty result as provider.ErrNoNodes.
func (p *Provider) nodePoolsForScale(
	ctx context.Context,
	clusterName string,
) ([]*containerpb.NodePool, string, error) {
	if p.client == nil {
		return nil, "", provider.ErrProviderUnavailable
	}

	location, err := p.clusterLocation(ctx, clusterName)
	if err != nil {
		return nil, "", err
	}

	cluster, err := p.client.GetCluster(ctx, p.project, location, clusterName)
	if err != nil {
		return nil, "", translateClientErr(err)
	}

	pools := cluster.GetNodePools()
	if len(pools) == 0 {
		return nil, "", provider.ErrNoNodes
	}

	return pools, location, nil
}

// clusterLocation resolves the location a cluster-scoped call should target.
// With a configured location it is used directly; with the all-locations
// wildcard (or empty) the cluster's own location is looked up via
// ListClusters, since cluster-scoped GKE calls reject the wildcard.
func (p *Provider) clusterLocation(ctx context.Context, clusterName string) (string, error) {
	if p.location != "" && p.location != AllLocations {
		return p.location, nil
	}

	clusters, err := p.client.ListClusters(ctx, p.project, AllLocations)
	if err != nil {
		return "", translateClientErr(err)
	}

	for _, cluster := range clusters {
		if cluster.GetName() == clusterName {
			return cluster.GetLocation(), nil
		}
	}

	return "", fmt.Errorf("%w: %s", provider.ErrClusterNotFound, clusterName)
}

// listLocation resolves the location for project-scoped listing, defaulting
// to the all-locations wildcard when none was configured.
func (p *Provider) listLocation() string {
	if p.location == "" {
		return AllLocations
	}

	return p.location
}

// nodePoolInfos maps a cluster's node pools to NodeInfo entries.
func nodePoolInfos(cluster *containerpb.Cluster, clusterName string) []provider.NodeInfo {
	pools := cluster.GetNodePools()
	nodes := make([]provider.NodeInfo, 0, len(pools))

	for _, pool := range pools {
		nodes = append(nodes, provider.NodeInfo{
			Name:        pool.GetName(),
			ClusterName: clusterName,
			Role:        nodeRoleWorker,
			State:       pool.GetStatus().String(),
			ServerType:  pool.GetConfig().GetMachineType(),
		})
	}

	return nodes
}

// translateClientErr maps GKE client errors onto the provider sentinels:
// a gRPC NotFound anywhere in the chain becomes provider.ErrClusterNotFound.
func translateClientErr(err error) error {
	if err == nil {
		return nil
	}

	var grpcErr interface{ GRPCStatus() *grpcstatus.Status }
	if errors.As(err, &grpcErr) && grpcErr.GRPCStatus().Code() == codes.NotFound {
		return fmt.Errorf("%w: %s", provider.ErrClusterNotFound, strings.TrimSpace(err.Error()))
	}

	return err
}
