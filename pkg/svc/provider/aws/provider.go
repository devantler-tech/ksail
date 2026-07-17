package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
)

// NodegroupStatusActive is the EKS nodegroup status that indicates the
// nodegroup is healthy and reconciling its desired capacity.
const NodegroupStatusActive = "ACTIVE"

// clusterDescriber is the narrow seam over the AWS-SDK EKS DescribeCluster
// operation the provider uses to read a cluster's control-plane endpoint —
// data eksctl's cluster summary does not carry. *pkg/client/eks.Client
// satisfies it, and tests inject a fake so no AWS credentials are needed.
type clusterDescriber interface {
	DescribeCluster(ctx context.Context, name string) (*ekstypes.Cluster, error)
}

// Provider implements provider.Provider for Amazon EKS via eksctl.
type Provider struct {
	client                  *eksctlclient.Client
	region                  string
	describer               clusterDescriber
	eksClientOptions        []eksclient.Option
	requireCredentialValues bool
}

// Option customises a Provider.
type Option func(*Provider)

// WithClusterDescriber injects the EKS DescribeCluster implementation, letting
// tests substitute a fake for the AWS-SDK-backed client the provider would
// otherwise construct lazily on the first GetClusterStatus call.
func WithClusterDescriber(describer clusterDescriber) Option {
	return func(p *Provider) {
		p.describer = describer
	}
}

// WithCredentialValues pins the credentials used by the provider's lazy
// AWS-SDK EKS client without mutating process environment. The eksctl client
// is configured separately with the matching canonical child environment.
func WithCredentialValues(profile, accessKeyID, secretAccessKey, sessionToken string) Option {
	return func(p *Provider) {
		p.eksClientOptions = []eksclient.Option{
			eksclient.WithCredentialValues(profile, accessKeyID, secretAccessKey, sessionToken),
		}
	}
}

// RequireCredentialValues prevents the lazy SDK client from falling back to
// ambient canonical credentials when custom sources resolved no values.
func RequireCredentialValues() Option {
	return func(p *Provider) {
		p.requireCredentialValues = true
	}
}

// NewProvider returns a Provider using the given eksctl client and AWS region.
// Pass region="" to defer to eksctl's own region resolution (AWS_REGION env,
// active AWS profile, etc.). A nil client returns ErrClientRequired so callers
// can fail fast rather than getting ErrProviderUnavailable at the first call.
func NewProvider(client *eksctlclient.Client, region string, opts ...Option) (*Provider, error) {
	if client == nil {
		return nil, ErrClientRequired
	}

	prov := &Provider{
		client:                  client,
		region:                  region,
		describer:               nil,
		eksClientOptions:        nil,
		requireCredentialValues: false,
	}

	for _, opt := range opts {
		opt(prov)
	}

	return prov, nil
}

// StartNodes restores every managed nodegroup to the exact desired/minimum/maximum values captured
// before StopNodes zeroed it. The snapshot survives partial failures and is removed only after a
// readback confirms that every group is restored.
func (p *Provider) StartNodes(ctx context.Context, clusterName string) error {
	nodegroups, err := p.listNodegroupsForScale(ctx, clusterName)
	if err != nil {
		return err
	}

	snapshot, found, err := p.loadNodegroupState(clusterName)
	if err != nil {
		return err
	}

	if !found {
		return p.startNodegroupsWithoutSnapshot(ctx, clusterName, nodegroups)
	}

	liveByName, err := validateNodegroupTransition(clusterName, p.region, snapshot, nodegroups)
	if err != nil {
		return err
	}

	err = p.restoreSavedNodegroups(ctx, clusterName, snapshot, liveByName)
	if err != nil {
		return err
	}

	return p.verifyAndClearNodegroupState(ctx, clusterName, snapshot)
}

// StopNodes atomically snapshots every managed nodegroup before scaling it to zero. Repeated calls
// preserve the first snapshot and skip already-stopped groups, so a partial stop remains retryable.
func (p *Provider) StopNodes(ctx context.Context, clusterName string) error {
	nodegroups, err := p.listNodegroupsForScale(ctx, clusterName)
	if err != nil {
		return err
	}

	snapshot, found, err := p.loadNodegroupState(clusterName)
	if err != nil {
		return err
	}

	if !found {
		snapshot, err = newNodegroupState(clusterName, p.region, nodegroups)
		if err != nil {
			return err
		}

		err = state.SaveEKSNodegroupState(clusterName, snapshot)
		if err != nil {
			return fmt.Errorf("save EKS nodegroup state before stop: %w", err)
		}
	}

	liveByName, err := validateNodegroupTransition(clusterName, p.region, snapshot, nodegroups)
	if err != nil {
		return err
	}

	for _, capacity := range snapshot.Nodegroups {
		if nodegroupIsStopped(liveByName[capacity.Name], capacity) {
			continue
		}

		err = p.client.ScaleNodegroup(
			ctx,
			clusterName,
			capacity.Name,
			p.region,
			0,
			0,
			capacity.MaxSize,
		)
		if err != nil {
			return fmt.Errorf("stop nodes: scale nodegroup %s: %w", capacity.Name, err)
		}
	}

	return nil
}

// ListNodes returns one NodeInfo per managed nodegroup for the cluster.
//
// EKS nodegroups are not individual nodes — they are ASG-backed groups — so
// KSail collapses the distinction and represents each nodegroup as a single
// NodeInfo whose Name is the nodegroup name. Downstream consumers that need
// instance-level detail should call the AWS SDK directly.
func (p *Provider) ListNodes(ctx context.Context, clusterName string) ([]provider.NodeInfo, error) {
	nodegroups, err := p.fetchNodegroups(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	nodes := make([]provider.NodeInfo, 0, len(nodegroups))

	for _, nodegroup := range nodegroups {
		nodes = append(nodes, provider.NodeInfo{
			Name:        nodegroup.Name,
			ClusterName: clusterName,
			Role:        classifyRole(nodegroup.NodeGroupType),
			State:       nodegroup.Status,
		})
	}

	return nodes, nil
}

// ListAllClusters returns the names of all EKS clusters visible in the
// configured region (or the default region eksctl resolves when region is "").
func (p *Provider) ListAllClusters(ctx context.Context) ([]string, error) {
	clusters, err := provider.FetchOrTranslate(
		p.client != nil,
		func() ([]eksctlclient.ClusterSummary, error) {
			return p.client.ListClusters(ctx, p.region)
		},
		translateClientErr,
	)
	if err != nil {
		return nil, err //nolint:wrapcheck // FetchOrTranslate already ran the error through translateClientErr
	}

	return provider.NamesFrom(
		clusters,
		func(c eksctlclient.ClusterSummary) string { return c.Name },
	), nil
}

// NodesExist returns true if the cluster has at least one managed nodegroup.
func (p *Provider) NodesExist(ctx context.Context, clusterName string) (bool, error) {
	exists, err := provider.CheckNodesExist(ctx, p, clusterName)
	if err != nil {
		return false, fmt.Errorf("check eks nodegroups: %w", err)
	}

	return exists, nil
}

// DeleteNodes is a no-op for EKS: `eksctl delete cluster` deletes all owned
// CloudFormation stacks (including managed nodegroups) atomically. Callers
// that need partial deletion should invoke the provisioner directly.
func (p *Provider) DeleteNodes(_ context.Context, _ string) error {
	return nil
}

// GetClusterStatus aggregates nodegroup statuses into a provider.ClusterStatus.
// Returns provider.ErrClusterNotFound when the cluster does not exist in EKS.
// A cluster that exists but has no managed nodegroups yields a zero-node
// "stopped" status rather than a nil status.
func (p *Provider) GetClusterStatus(
	ctx context.Context,
	clusterName string,
) (*provider.ClusterStatus, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	_, err := p.client.GetCluster(ctx, clusterName, p.region)
	if err != nil {
		return nil, translateClientErr(err)
	}

	nodes, err := p.ListNodes(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("get cluster status: %w", err)
	}

	status := provider.BuildClusterStatus(nodes, NodegroupStatusActive)
	if status == nil {
		// BuildClusterStatus returns nil for an empty node list, but the
		// cluster exists (GetCluster succeeded above) — only its nodegroups
		// are absent. Return a zero-node status so callers can rely on the
		// (status, err) contract: status is non-nil whenever err is nil.
		status = &provider.ClusterStatus{Phase: provider.PhaseStopped, Nodes: nodes}
	}

	endpoint, err := p.clusterEndpoint(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	status.Endpoint = endpoint

	return status, nil
}

// Region returns the AWS region this provider was configured with.
func (p *Provider) Region() string {
	return p.region
}

// clusterEndpoint reads the cluster's control-plane endpoint from the AWS SDK,
// which eksctl's cluster summary omits — bringing EKS to parity with the GKE
// and Azure providers, which both surface the endpoint on their status. The
// SDK-backed describer is resolved lazily so the eksctl-only lifecycle paths
// never resolve AWS credentials.
func (p *Provider) clusterEndpoint(ctx context.Context, clusterName string) (string, error) {
	describer, err := p.resolveDescriber(ctx)
	if err != nil {
		return "", fmt.Errorf("get cluster status: %w", err)
	}

	cluster, err := describer.DescribeCluster(ctx, clusterName)
	if err != nil {
		return "", fmt.Errorf("get cluster status: describe eks cluster: %w", err)
	}

	return awssdk.ToString(cluster.Endpoint), nil
}

// resolveDescriber returns the injected describer, or lazily constructs the
// AWS-SDK-backed EKS client when none was injected. Construction is deferred
// to first use because the eksctl lifecycle paths never need AWS credentials
// resolved; GetClusterStatus is called once per `cluster info`, so no caching
// of the constructed client is warranted.
func (p *Provider) resolveDescriber(ctx context.Context) (clusterDescriber, error) {
	if p.describer != nil {
		return p.describer, nil
	}

	client, err := eksclient.NewClientWithCredentialRequirement(
		ctx,
		p.region,
		p.requireCredentialValues,
		p.eksClientOptions...,
	)
	if err != nil {
		return nil, fmt.Errorf("creating aws eks client: %w", err)
	}

	return client, nil
}

// fetchNodegroups guards against a nil client and fetches this cluster's nodegroups — the
// exec/error-handling shared by ListNodes and listNodegroupsForScale before they diverge on how
// they treat an empty result.
func (p *Provider) fetchNodegroups(
	ctx context.Context,
	clusterName string,
) ([]eksctlclient.NodegroupSummary, error) {
	nodegroups, err := provider.FetchOrTranslate(
		p.client != nil,
		func() ([]eksctlclient.NodegroupSummary, error) {
			return p.client.ListNodegroups(ctx, clusterName, p.region)
		},
		translateClientErr,
	)
	if err != nil {
		return nil, err //nolint:wrapcheck // FetchOrTranslate already ran the error through translateClientErr
	}

	return nodegroups, nil
}

// listNodegroupsForScale is a shared prelude for StartNodes and StopNodes
// that guards against nil clients, fetches nodegroups, and classifies the
// empty result as provider.ErrNoNodes.
func (p *Provider) listNodegroupsForScale(
	ctx context.Context,
	clusterName string,
) ([]eksctlclient.NodegroupSummary, error) {
	nodegroups, err := p.fetchNodegroups(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	if len(nodegroups) == 0 {
		return nil, provider.ErrNoNodes
	}

	return nodegroups, nil
}

// classifyRole maps eksctl's NodeGroupType values ("managed", "unmanaged",
// "self-managed") to the KSail role taxonomy. EKS nodegroups are workers;
// the control plane is fully managed by AWS.
func classifyRole(_ string) string {
	return "worker"
}

// translateClientErr maps eksctl client errors onto provider-level errors so
// callers can rely on errors.Is(err, provider.ErrClusterNotFound) etc.
func translateClientErr(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, eksctlclient.ErrClusterNotFound) {
		return fmt.Errorf("%w: %s", provider.ErrClusterNotFound, strings.TrimSpace(err.Error()))
	}

	return err
}
