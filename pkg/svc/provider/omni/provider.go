package omni

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/blang/semver/v4"
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
	st     state.State
}

// NewProvider creates a new Omni provider with the given client.
func NewProvider(client *omniclient.Client) *Provider {
	if client == nil {
		return &Provider{}
	}

	return &Provider{
		client: client,
		st:     client.Omni().State(),
	}
}

// NewProviderWithState creates a Provider with an injected COSI state.
// This is primarily useful for testing and dependency injection scenarios
// where a real Omni client is not available.
func NewProviderWithState(st state.State) *Provider {
	return &Provider{st: st}
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
	if p.st == nil {
		return nil, provider.ErrProviderUnavailable
	}

	machines, err := safe.StateListAll[*omnires.ClusterMachineStatus](
		ctx,
		p.st,
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
	if p.st == nil {
		return nil, provider.ErrProviderUnavailable
	}

	clusters, err := safe.StateListAll[*omnires.Cluster](ctx, p.st)
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
	if p.st == nil {
		return provider.ErrProviderUnavailable
	}

	// Use TeardownAndDestroy to follow the COSI lifecycle:
	// 1. Initiates teardown (transitions resource to PhaseTearingDown)
	// 2. Blocks until all controllers remove their finalizers
	// 3. Destroys the resource once finalizer list is empty
	cluster := omnires.NewCluster(clusterName)

	err := p.st.TeardownAndDestroy(ctx, cluster.Metadata())
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil
		}

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
	if p.st == nil {
		return false, provider.ErrProviderUnavailable
	}

	cluster := omnires.NewCluster(clusterName)

	_, err := safe.StateGet[*omnires.Cluster](ctx, p.st, cluster.Metadata())
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
func (p *Provider) CreateCluster(
	ctx context.Context,
	templateReader io.Reader,
	out io.Writer,
) error {
	if p.st == nil {
		return provider.ErrProviderUnavailable
	}

	if templateReader == nil {
		return ErrTemplateReaderRequired
	}

	if out == nil {
		out = io.Discard
	}

	err := operations.SyncTemplate(ctx, templateReader, out, p.st, operations.SyncOptions{})
	if err != nil {
		return fmt.Errorf("failed to sync template to Omni: %w", err)
	}

	return nil
}

// clusterReadyPollInterval is the interval between cluster readiness polls.
const clusterReadyPollInterval = 10 * time.Second

// WaitForClusterReady polls the ClusterStatus resource until the cluster is ready
// (Phase == RUNNING and Ready == true) or the timeout expires.
func (p *Provider) WaitForClusterReady(
	ctx context.Context,
	clusterName string,
	timeout time.Duration,
) error {
	if p.st == nil {
		return provider.ErrProviderUnavailable
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(clusterReadyPollInterval)
	defer ticker.Stop()

	for {
		ready, err := isClusterRunningAndReady(ctx, p.st, clusterName)
		if err != nil {
			return err
		}

		if ready {
			return nil
		}

		select {
		case <-ctx.Done():
			ctxErr := ctx.Err()
			if errors.Is(ctxErr, context.Canceled) {
				return fmt.Errorf(
					"cancelled waiting for cluster %q to become ready: %w",
					clusterName,
					ctxErr,
				)
			}

			return fmt.Errorf(
				"timed out waiting for cluster %q to become ready: %w",
				clusterName,
				ctxErr,
			)
		case <-ticker.C:
		}
	}
}

// isClusterRunningAndReady checks whether the Omni cluster has Phase==RUNNING and Ready==true.
// It returns (false, nil) when the cluster resource is not yet found or when the context
// is cancelled/expired, allowing the caller to retry or handle the context via ctx.Done().
func isClusterRunningAndReady(
	ctx context.Context,
	omniState state.State,
	clusterName string,
) (bool, error) {
	status, err := safe.StateGet[*omnires.ClusterStatus](
		ctx,
		omniState,
		omnires.NewClusterStatus(clusterName).Metadata(),
	)
	if err != nil {
		if state.IsNotFoundError(err) ||
			errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(err, context.Canceled) {
			return false, nil
		}

		return false, fmt.Errorf("failed to get cluster status for %q: %w", clusterName, err)
	}

	phase := status.TypedSpec().Value.GetPhase()
	ready := status.TypedSpec().Value.GetReady()

	return phase == specs.ClusterStatusSpec_RUNNING && ready, nil
}

// GetKubeconfig retrieves the kubeconfig for the given cluster from Omni.
func (p *Provider) GetKubeconfig(ctx context.Context, clusterName string) ([]byte, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	data, err := p.client.Management().WithCluster(clusterName).Kubeconfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig from Omni: %w", err)
	}

	return data, nil
}

// GetTalosconfig retrieves the talosconfig for the given cluster from Omni.
func (p *Provider) GetTalosconfig(ctx context.Context, clusterName string) ([]byte, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	data, err := p.client.Management().WithCluster(clusterName).Talosconfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get talosconfig from Omni: %w", err)
	}

	return data, nil
}

// LatestTalosVersion queries Omni for the latest non-deprecated TalosVersion resource.
// It returns the version ID (e.g. "1.12.4") and its list of compatible Kubernetes versions.
func (p *Provider) LatestTalosVersion(ctx context.Context) (string, []string, error) {
	if p.st == nil {
		return "", nil, provider.ErrProviderUnavailable
	}

	versions, err := safe.StateListAll[*omnires.TalosVersion](ctx, p.st)
	if err != nil {
		return "", nil, fmt.Errorf("failed to list Talos versions from Omni: %w", err)
	}

	var latestID string
	var latestSemver semver.Version
	var latestK8s []string
	found := false

	for ver := range versions.All() {
		if ver.TypedSpec().Value.GetDeprecated() {
			continue
		}

		id := ver.Metadata().ID()

		parsed, parseErr := semver.Parse(strings.TrimPrefix(id, "v"))
		if parseErr != nil {
			continue
		}

		if !found || parsed.GT(latestSemver) {
			latestID = id
			latestSemver = parsed
			latestK8s = ver.TypedSpec().Value.GetCompatibleKubernetesVersions()
			found = true
		}
	}

	if !found {
		return "", nil, ErrNoTalosVersions
	}

	return latestID, latestK8s, nil
}

// IsAvailable returns true if the provider is ready for use.
func (p *Provider) IsAvailable() bool {
	return p.client != nil && p.st != nil
}
