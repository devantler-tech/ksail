package omni

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/siderolabs/omni/client/api/omni/specs"
	omniclient "github.com/siderolabs/omni/client/pkg/client"
	omnires "github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"github.com/siderolabs/omni/client/pkg/template/operations"
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

// Client returns the underlying Omni client for direct API access.
func (p *Provider) Client() *omniclient.Client {
	return p.client
}

// ClusterExists returns true if a Cluster resource exists in Omni for the given name.
// This checks for the Cluster resource itself, not nodes — a newly created cluster
// may not have nodes allocated yet.
func (p *Provider) ClusterExists(ctx context.Context, clusterName string) (bool, error) {
	if p.client == nil {
		return false, provider.ErrProviderUnavailable
	}

	st := p.client.Omni().State()

	cluster := omnires.NewCluster(clusterName)

	_, err := safe.StateGet[*omnires.Cluster](ctx, st, cluster.Metadata())
	if err != nil {
		if state.IsNotFoundError(err) {
			return false, nil
		}

		return false, fmt.Errorf("failed to check cluster existence: %w", err)
	}

	return true, nil
}

// CreateCluster creates a cluster in Omni by syncing a cluster template.
// The templateReader should contain a multi-document YAML cluster template
// (Cluster + ControlPlane + Workers kinds) compatible with the Omni template format.
func (p *Provider) CreateCluster(ctx context.Context, templateReader io.Reader, out io.Writer) error {
	if p.client == nil {
		return provider.ErrProviderUnavailable
	}

	st := p.client.Omni().State()

	return operations.SyncTemplate(ctx, templateReader, out, st, operations.SyncOptions{})
}

// clusterReadyPollInterval is the interval between cluster readiness polls.
const clusterReadyPollInterval = 10 * time.Second

// WaitForClusterReady polls the ClusterStatus resource until the cluster is ready
// (Phase == RUNNING and Ready == true) or the timeout expires.
func (p *Provider) WaitForClusterReady(ctx context.Context, clusterName string, timeout time.Duration) error {
	if p.client == nil {
		return provider.ErrProviderUnavailable
	}

	st := p.client.Omni().State()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(clusterReadyPollInterval)
	defer ticker.Stop()

	for {
		status, err := safe.StateGet[*omnires.ClusterStatus](
			ctx,
			st,
			omnires.NewClusterStatus(clusterName).Metadata(),
		)
		if err == nil {
			phase := status.TypedSpec().Value.GetPhase()
			ready := status.TypedSpec().Value.GetReady()

			if phase == specs.ClusterStatusSpec_RUNNING && ready {
				return nil
			}
		} else if !state.IsNotFoundError(err) {
			return fmt.Errorf("failed to get cluster status for %q: %w", clusterName, err)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for cluster %q to become ready: %w", clusterName, ctx.Err())
		case <-ticker.C:
		}
	}
}

// GetKubeconfig retrieves the kubeconfig for the given cluster from Omni.
func (p *Provider) GetKubeconfig(ctx context.Context, clusterName string) ([]byte, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	return p.client.Management().WithCluster(clusterName).Kubeconfig(ctx)
}

// GetTalosconfig retrieves the talosconfig for the given cluster from Omni.
func (p *Provider) GetTalosconfig(ctx context.Context, clusterName string) ([]byte, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	return p.client.Management().WithCluster(clusterName).Talosconfig(ctx)
}

// IsAvailable returns true if the provider is ready for use.
func (p *Provider) IsAvailable() bool {
	return p.client != nil
}
