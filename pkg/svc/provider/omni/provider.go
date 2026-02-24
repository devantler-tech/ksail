package omni

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	omniclient "github.com/siderolabs/omni/client/pkg/client"
	omnires "github.com/siderolabs/omni/client/pkg/omni/resources/omni"
)

// Provider implements provider.Provider for Sidero Omni managed Talos clusters.
type Provider struct {
	client *omniclient.Client
}

// NewProvider creates a new Omni provider with the given client.
func NewProvider(client *omniclient.Client) *Provider {
	return &Provider{
		client: client,
	}
}

// StartNodes is a no-op for Omni. Omni manages the machine lifecycle
// and nodes are automatically started when allocated to a cluster.
// It validates that the cluster has nodes and returns provider.ErrNoNodes
// when no nodes exist for the given cluster.
func (p *Provider) StartNodes(ctx context.Context, clusterName string) error {
	nodes, err := p.ListNodes(ctx, clusterName)
	if err != nil {
		return err
	}

	if len(nodes) == 0 {
		return provider.ErrNoNodes
	}

	return nil
}

// StopNodes is a no-op for Omni. Omni manages the machine lifecycle
// and nodes cannot be individually stopped through the provider.
// It validates that the cluster has nodes and returns provider.ErrNoNodes
// when no nodes exist for the given cluster.
func (p *Provider) StopNodes(ctx context.Context, clusterName string) error {
	nodes, err := p.ListNodes(ctx, clusterName)
	if err != nil {
		return err
	}

	if len(nodes) == 0 {
		return provider.ErrNoNodes
	}

	return nil
}

// ListNodes returns all machines allocated to the given cluster in Omni.
func (p *Provider) ListNodes(ctx context.Context, clusterName string) ([]provider.NodeInfo, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	st := p.client.Omni().State()

	machines, err := safe.StateListAll[*omnires.ClusterMachineStatus](
		ctx,
		st,
		state.WithLabelQuery(resource.LabelEqual(omnires.LabelCluster, clusterName)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list cluster machines: %w", err)
	}

	nodes := make([]provider.NodeInfo, 0, machines.Len())

	for machine := range machines.All() {
		role := "worker"
		if _, ok := machine.Metadata().Labels().Get(omnires.LabelControlPlaneRole); ok {
			role = "controlplane"
		}

		stage := "unknown"
		if machine.TypedSpec().Value.GetStage() != 0 {
			stage = machine.TypedSpec().Value.GetStage().String()
		}

		nodes = append(nodes, provider.NodeInfo{
			Name:        machine.Metadata().ID(),
			ClusterName: clusterName,
			Role:        role,
			State:       stage,
		})
	}

	return nodes, nil
}

// ListAllClusters returns the names of all clusters managed in Omni.
func (p *Provider) ListAllClusters(ctx context.Context) ([]string, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	st := p.client.Omni().State()

	clusters, err := safe.StateListAll[*omnires.Cluster](ctx, st)
	if err != nil {
		return nil, fmt.Errorf("failed to list clusters: %w", err)
	}

	names := make([]string, 0, clusters.Len())

	for cluster := range clusters.All() {
		names = append(names, cluster.Metadata().ID())
	}

	return names, nil
}

// NodesExist returns true if machines are allocated to the given cluster in Omni.
func (p *Provider) NodesExist(ctx context.Context, clusterName string) (bool, error) {
	exists, err := provider.CheckNodesExist(ctx, p, clusterName)
	if err != nil {
		return false, fmt.Errorf("omni: check nodes exist: %w", err)
	}

	return exists, nil
}

// DeleteNodes removes all machines for the given cluster by deleting the cluster in Omni.
// This deallocates machines from the cluster but does not destroy the physical machines.
func (p *Provider) DeleteNodes(ctx context.Context, clusterName string) error {
	if p.client == nil {
		return provider.ErrProviderUnavailable
	}

	st := p.client.Omni().State()

	// Delete the cluster resource which deallocates all machines
	cluster := omnires.NewCluster(clusterName)

	err := st.Destroy(ctx, cluster.Metadata())
	if err != nil {
		return fmt.Errorf("failed to delete cluster %s: %w", clusterName, err)
	}

	return nil
}

// IsAvailable returns true if the provider is ready for use.
func (p *Provider) IsAvailable() bool {
	return p.client != nil
}
