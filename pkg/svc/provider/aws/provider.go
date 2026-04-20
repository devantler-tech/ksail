package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"

	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
)

// NodegroupStatusActive is the EKS nodegroup status that indicates the
// nodegroup is healthy and reconciling its desired capacity.
const NodegroupStatusActive = "ACTIVE"

// Provider implements provider.Provider for Amazon EKS via eksctl.
type Provider struct {
	client *eksctlclient.Client
	region string
}

// NewProvider returns a Provider using the given eksctl client and AWS region.
// Pass region="" to defer to eksctl's own region resolution (AWS_REGION env,
// active AWS profile, etc.). A nil client returns ErrClientRequired so callers
// can fail fast rather than getting ErrProviderUnavailable at the first call.
func NewProvider(client *eksctlclient.Client, region string) (*Provider, error) {
	if client == nil {
		return nil, ErrClientRequired
	}

	return &Provider{client: client, region: region}, nil
}

// StartNodes scales all managed nodegroups for the cluster back to their
// configured desired capacity if it is currently zero. Nodegroups already at
// non-zero desired capacity are left alone.
//
// When a nodegroup's desired capacity is zero, this method does not know the
// "correct" target size to scale up to, so it defers to the max(MinSize, 1)
// that eksctl returned. The provisioner — which has the ksail.yaml spec — is
// responsible for restoring the exact DesiredCapacity when needed.
func (p *Provider) StartNodes(ctx context.Context, clusterName string) error {
	nodegroups, err := p.listNodegroupsForScale(ctx, clusterName)
	if err != nil {
		return err
	}

	for _, nodegroup := range nodegroups {
		if nodegroup.DesiredCap > 0 {
			continue
		}

		target := max(nodegroup.MinSize, 1)

		err = p.client.ScaleNodegroup(
			ctx,
			clusterName,
			nodegroup.Name,
			p.region,
			target,
			nodegroup.MinSize,
			nodegroup.MaxSize,
		)
		if err != nil {
			return fmt.Errorf("start nodes: scale nodegroup %s: %w", nodegroup.Name, err)
		}
	}

	return nil
}

// StopNodes scales all managed nodegroups to zero desired capacity. The
// cluster control plane remains running (EKS bills $0.10/hour for it) but
// all node-hour costs stop.
func (p *Provider) StopNodes(ctx context.Context, clusterName string) error {
	nodegroups, err := p.listNodegroupsForScale(ctx, clusterName)
	if err != nil {
		return err
	}

	for _, nodegroup := range nodegroups {
		err = p.client.ScaleNodegroup(
			ctx,
			clusterName,
			nodegroup.Name,
			p.region,
			0,
			0,
			nodegroup.MaxSize,
		)
		if err != nil {
			return fmt.Errorf("stop nodes: scale nodegroup %s: %w", nodegroup.Name, err)
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
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	nodegroups, err := p.client.ListNodegroups(ctx, clusterName, p.region)
	if err != nil {
		return nil, translateClientErr(err)
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
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	clusters, err := p.client.ListClusters(ctx, p.region)
	if err != nil {
		return nil, translateClientErr(err)
	}

	names := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		names = append(names, cluster.Name)
	}

	return names, nil
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

	return provider.BuildClusterStatus(nodes, NodegroupStatusActive), nil
}

// Region returns the AWS region this provider was configured with.
func (p *Provider) Region() string {
	return p.region
}

// listNodegroupsForScale is a shared prelude for StartNodes and StopNodes
// that guards against nil clients, fetches nodegroups, and classifies the
// empty result as provider.ErrNoNodes.
func (p *Provider) listNodegroupsForScale(
	ctx context.Context,
	clusterName string,
) ([]eksctlclient.NodegroupSummary, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	nodegroups, err := p.client.ListNodegroups(ctx, clusterName, p.region)
	if err != nil {
		return nil, translateClientErr(err)
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
