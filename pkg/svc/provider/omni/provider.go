package omni

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	msemver "github.com/Masterminds/semver/v3"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/siderolabs/omni/client/api/omni/specs"
	omniclient "github.com/siderolabs/omni/client/pkg/client"
	"github.com/siderolabs/omni/client/pkg/client/management"
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

// WaitForClusterReady polls the ClusterStatus resource until the cluster is ready
// (Phase == RUNNING and Ready == true) or the timeout expires.
func (p *Provider) WaitForClusterReady(
	ctx context.Context,
	clusterName string,
	timeout time.Duration,
) error {
	return p.waitForCluster(ctx, clusterName, timeout, isClusterRunningAndReady, "become ready")
}

// WaitForClusterRunning polls the ClusterStatus resource until the cluster phase
// is RUNNING or the timeout expires. Unlike WaitForClusterReady, this does NOT
// require Ready==true, which depends on all nodes being Ready in Kubernetes.
// Nodes cannot become Ready until a CNI is installed, so this method is used
// during cluster creation when CNI installation happens as a post-creation step.
func (p *Provider) WaitForClusterRunning(
	ctx context.Context,
	clusterName string,
	timeout time.Duration,
) error {
	return p.waitForCluster(ctx, clusterName, timeout, isClusterRunning, "reach RUNNING phase")
}

// DefaultKubeconfigTTL is the default time-to-live for Omni service-account
// kubeconfig tokens. Tokens are automatically refreshed by the CLI's
// PersistentPreRunE hook when they expire; the 30-day default keeps
// refresh frequency low while remaining safe for most workflows.
const DefaultKubeconfigTTL = 30 * 24 * time.Hour

// GetKubeconfig retrieves the kubeconfig for the given cluster from Omni.
// It requests a service-account kubeconfig with a static token, which works
// in non-interactive environments (CI, automation) without requiring oidc-login.
//
// If ttl is <= 0, DefaultKubeconfigTTL (30 days) is used.
// The CLI's PersistentPreRunE hook transparently refreshes the kubeconfig
// before any command when the token has expired.
func (p *Provider) GetKubeconfig(
	ctx context.Context,
	clusterName string,
	ttl time.Duration,
) ([]byte, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	if ttl <= 0 {
		ttl = DefaultKubeconfigTTL
	}

	data, err := p.client.Management().WithCluster(clusterName).Kubeconfig(
		ctx,
		management.WithServiceAccount(ttl, "ksail", "system:masters"),
	)
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

	var latestSemver *msemver.Version

	var latestK8s []string

	for ver := range versions.All() {
		if ver.TypedSpec().Value.GetDeprecated() {
			continue
		}

		versionID := ver.Metadata().ID()

		parsed, parseErr := msemver.NewVersion(strings.TrimPrefix(versionID, "v"))
		if parseErr != nil {
			continue
		}

		if latestSemver == nil || parsed.GreaterThan(latestSemver) {
			latestID = versionID
			latestSemver = parsed
			latestK8s = ver.TypedSpec().Value.GetCompatibleKubernetesVersions()
		}
	}

	if latestSemver == nil {
		return "", nil, ErrNoTalosVersions
	}

	return latestID, latestK8s, nil
}

// IsAvailable returns true if the provider is ready for use.
// Only COSI state is required — client-dependent methods (GetKubeconfig,
// GetTalosconfig, Client) nil-guard on the client field independently.
func (p *Provider) IsAvailable() bool {
	return p.st != nil
}

// GetClusterStatus returns the provider-level status of an Omni-managed cluster.
// It queries the ClusterStatus COSI resource for phase, readiness, and machine counts,
// and lists individual machine nodes.
func (p *Provider) GetClusterStatus(
	ctx context.Context,
	clusterName string,
) (*provider.ClusterStatus, error) {
	if p.st == nil {
		return nil, provider.ErrProviderUnavailable
	}

	exists, err := p.ClusterExists(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("check cluster existence: %w", err)
	}

	if !exists {
		return nil, fmt.Errorf("%w: %s", provider.ErrClusterNotFound, clusterName)
	}

	fields, statusErr := p.getClusterStatusSpec(ctx, clusterName)

	nodes, nodesErr := p.ListNodes(ctx, clusterName)

	switch {
	case nodesErr != nil && statusErr != nil:
		return nil, fmt.Errorf(
			"get cluster status: %w", errors.Join(statusErr, nodesErr),
		)
	case statusErr != nil:
		total, ready := countReadyNodes(nodes)
		fields = clusterStatusFields{
			phase:      "UNKNOWN",
			ready:      ready == total && total > 0,
			nodesTotal: total,
			nodesReady: ready,
		}
	}

	return &provider.ClusterStatus{
		Phase:      fields.phase,
		Ready:      fields.ready,
		NodesTotal: fields.nodesTotal,
		NodesReady: fields.nodesReady,
		Nodes:      nodes,
		Endpoint:   p.endpoint(),
	}, nil
}

// ListAvailableMachines queries Omni for machines that are available (not allocated
// to any cluster) and returns exactly count machine UUIDs on success.
// Returns ErrInsufficientAvailableMachines when fewer than count machines are available.
// Returns a validation error if count is negative, and an empty slice if count is zero.
func (p *Provider) ListAvailableMachines(ctx context.Context, count int) ([]string, error) {
	if p.st == nil {
		return nil, provider.ErrProviderUnavailable
	}

	if count < 0 {
		return nil, fmt.Errorf("%w: %d", ErrNegativeMachineCount, count)
	}

	if count == 0 {
		return []string{}, nil
	}

	machines, err := safe.StateListAll[*omnires.MachineStatus](
		ctx,
		p.st,
		state.WithLabelQuery(resource.LabelExists(omnires.MachineStatusLabelAvailable)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list available machines: %w", err)
	}

	ids := make([]string, 0, min(count, machines.Len()))

	for machine := range machines.All() {
		if len(ids) >= count {
			break
		}

		ids = append(ids, machine.Metadata().ID())
	}

	if len(ids) < count {
		return nil, fmt.Errorf(
			"%w: need %d, got %d",
			ErrInsufficientAvailableMachines,
			count, len(ids),
		)
	}

	return ids, nil
}

// clusterStatusFields holds the parsed COSI cluster status fields.
type clusterStatusFields struct {
	phase      string
	ready      bool
	nodesTotal int
	nodesReady int
}

// getClusterStatusSpec fetches the COSI ClusterStatus resource fields.
func (p *Provider) getClusterStatusSpec(
	ctx context.Context,
	clusterName string,
) (clusterStatusFields, error) {
	status, err := safe.StateGet[*omnires.ClusterStatus](
		ctx,
		p.st,
		omnires.NewClusterStatus(clusterName).Metadata(),
	)
	if err != nil {
		return clusterStatusFields{},
			fmt.Errorf("get cluster status spec: %w", err)
	}

	fields := clusterStatusFields{
		phase: status.TypedSpec().Value.GetPhase().String(),
		ready: status.TypedSpec().Value.GetReady(),
	}

	if machines := status.TypedSpec().Value.GetMachines(); machines != nil {
		fields.nodesTotal = int(machines.GetTotal())
		fields.nodesReady = int(machines.GetHealthy())
	}

	return fields, nil
}

// endpoint returns the Omni API endpoint URL if available.
func (p *Provider) endpoint() string {
	if p.client == nil {
		return ""
	}

	return p.client.Endpoint()
}

// countReadyNodes derives node counts from the node list.
func countReadyNodes(nodes []provider.NodeInfo) (int, int) {
	total := len(nodes)
	ready := 0

	for _, n := range nodes {
		if n.State == specs.ClusterMachineStatusSpec_RUNNING.String() {
			ready++
		}
	}

	return total, ready
}

// clusterStatusPollInterval is the interval between cluster status polls.
const clusterStatusPollInterval = 10 * time.Second

// clusterCheckFunc checks a cluster condition and returns true when the condition is met.
type clusterCheckFunc func(ctx context.Context, st state.State, clusterName string) (bool, error)

// waitForCluster polls a cluster condition until it is met or the timeout expires.
func (p *Provider) waitForCluster(
	ctx context.Context,
	clusterName string,
	timeout time.Duration,
	check clusterCheckFunc,
	conditionLabel string,
) error {
	if p.st == nil {
		return provider.ErrProviderUnavailable
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(clusterStatusPollInterval)
	defer ticker.Stop()

	for {
		ok, err := check(ctx, p.st, clusterName)
		if err != nil {
			return err
		}

		if ok {
			return nil
		}

		select {
		case <-ctx.Done():
			ctxErr := ctx.Err()
			if errors.Is(ctxErr, context.Canceled) {
				return fmt.Errorf(
					"cancelled waiting for cluster %q to %s: %w",
					clusterName, conditionLabel, ctxErr,
				)
			}

			return fmt.Errorf(
				"timed out waiting for cluster %q to %s: %w",
				clusterName, conditionLabel, ctxErr,
			)
		case <-ticker.C:
		}
	}
}

// clusterStatusPredicate evaluates a condition on a ClusterStatusSpec.
type clusterStatusPredicate func(phase specs.ClusterStatusSpec_Phase, ready bool) bool

// checkClusterStatus fetches the ClusterStatus and evaluates the predicate.
// It returns (false, nil) when the resource is not yet found or when the context
// is cancelled/expired, allowing the caller to retry or handle the context via ctx.Done().
func checkClusterStatus(
	ctx context.Context,
	omniState state.State,
	clusterName string,
	predicate clusterStatusPredicate,
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

	return predicate(
		status.TypedSpec().Value.GetPhase(),
		status.TypedSpec().Value.GetReady(),
	), nil
}

// isClusterRunningAndReady checks whether the Omni cluster has Phase==RUNNING and Ready==true.
func isClusterRunningAndReady(
	ctx context.Context,
	omniState state.State,
	clusterName string,
) (bool, error) {
	return checkClusterStatus(
		ctx,
		omniState,
		clusterName,
		func(phase specs.ClusterStatusSpec_Phase, ready bool) bool {
			return phase == specs.ClusterStatusSpec_RUNNING && ready
		},
	)
}

// isClusterRunning checks whether the Omni cluster has Phase==RUNNING (regardless of Ready).
func isClusterRunning(
	ctx context.Context,
	omniState state.State,
	clusterName string,
) (bool, error) {
	return checkClusterStatus(
		ctx,
		omniState,
		clusterName,
		func(phase specs.ClusterStatusSpec_Phase, _ bool) bool {
			return phase == specs.ClusterStatusSpec_RUNNING
		},
	)
}
