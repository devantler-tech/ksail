package azure

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	armcontainerservice "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
	"github.com/devantler-tech/ksail/v7/pkg/client/aks"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
)

// nodeRoleWorker is the role every AKS agent pool maps to: the control plane
// is Azure-managed and never surfaces as a pool.
const nodeRoleWorker = "worker"

// agentPoolReadyState is the ARM provisioning state of a healthy agent pool —
// the AKS counterpart to a RUNNING GKE node pool.
const agentPoolReadyState = "Succeeded"

// ClusterClient is the slice of the pkg/client/aks surface the provider
// drives. It is the provider's test seam: the aks client has no injectable
// manager (its own tests fake the ARM transport), so the provider narrows the
// dependency to the three calls it makes instead.
type ClusterClient interface {
	// GetCluster fetches one managed cluster in a resource group.
	GetCluster(
		ctx context.Context,
		resourceGroup, name string,
	) (armcontainerservice.ManagedCluster, error)
	// ListClusters lists the clusters in a resource group, or across the whole
	// subscription when resourceGroup is empty.
	ListClusters(
		ctx context.Context,
		resourceGroup string,
	) ([]*armcontainerservice.ManagedCluster, error)
	// SetAgentPoolCount resizes one agent pool of a cluster.
	SetAgentPoolCount(
		ctx context.Context,
		resourceGroup, clusterName, poolName string,
		count int32,
	) error
}

// staticClusterClientCheck asserts at compile time that the production aks
// client satisfies the provider's seam.
var _ ClusterClient = (*aks.Client)(nil)

// Provider implements provider.Provider for Azure Kubernetes Service.
type Provider struct {
	client        ClusterClient
	resourceGroup string
}

// NewProvider returns a Provider for the given resource group using the given
// AKS client. Pass resourceGroup="" to operate across the whole subscription
// where the API allows it (cluster listing); cluster-scoped calls then resolve
// the cluster's own resource group from its ARM ID via a subscription-wide
// list. A nil client returns ErrClientRequired so callers fail fast rather
// than at the first API call.
func NewProvider(client ClusterClient, resourceGroup string) (*Provider, error) {
	if client == nil {
		return nil, ErrClientRequired
	}

	return &Provider{client: client, resourceGroup: resourceGroup}, nil
}

// StartNodes resizes every agent pool of the cluster back to at least one
// node: the pool's current count when it still has one, its autoscaler
// minimum when one is configured, and one node otherwise.
//
// AKS does not preserve a creation-time count on the agent-pool profile (the
// profile's Count is the live size, zero after StopNodes), so unlike GKE
// there is no initial count to restore — the resize is re-asserted for every
// pool, which the API treats as a no-op when the pool is already at that
// size. The provisioner — which has the ksail.yaml spec — is responsible for
// restoring an exact desired size when needed.
func (p *Provider) StartNodes(ctx context.Context, clusterName string) error {
	pools, resourceGroup, err := p.agentPoolsForScale(ctx, clusterName)
	if err != nil {
		return err
	}

	for _, pool := range pools {
		target := max(derefInt32(pool.Count), derefInt32(pool.MinCount), 1)

		err = p.client.SetAgentPoolCount(
			ctx, resourceGroup, clusterName, derefString(pool.Name), target,
		)
		if err != nil {
			return fmt.Errorf(
				"start nodes: resize agent pool %s: %w", derefString(pool.Name), err,
			)
		}
	}

	return nil
}

// StopNodes resizes every agent pool of the cluster to zero nodes. The AKS
// control plane keeps running (and billing) but all node costs stop.
func (p *Provider) StopNodes(ctx context.Context, clusterName string) error {
	pools, resourceGroup, err := p.agentPoolsForScale(ctx, clusterName)
	if err != nil {
		return err
	}

	for _, pool := range pools {
		err = p.client.SetAgentPoolCount(
			ctx, resourceGroup, clusterName, derefString(pool.Name), 0,
		)
		if err != nil {
			return fmt.Errorf(
				"stop nodes: resize agent pool %s: %w", derefString(pool.Name), err,
			)
		}
	}

	return nil
}

// ListNodes returns one NodeInfo per agent pool of the cluster.
//
// AKS agent pools are managed scale sets, not individual machines, so KSail
// collapses the distinction and represents each pool as a single NodeInfo
// whose Name is the pool name — mirroring how the gcp provider represents GKE
// node pools and the AWS provider represents EKS nodegroups.
func (p *Provider) ListNodes(
	ctx context.Context,
	clusterName string,
) ([]provider.NodeInfo, error) {
	cluster, err := p.getCluster(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	return agentPoolInfos(cluster, clusterName), nil
}

// ListAllClusters returns the names of all AKS clusters in the configured
// resource group (the whole subscription when the provider was built with an
// empty resource group).
func (p *Provider) ListAllClusters(ctx context.Context) ([]string, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	clusters, err := p.client.ListClusters(ctx, p.resourceGroup)
	if err != nil {
		return nil, translateClientErr(err)
	}

	names := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		names = append(names, derefString(cluster.Name))
	}

	return names, nil
}

// NodesExist returns true if the cluster has at least one agent pool.
func (p *Provider) NodesExist(ctx context.Context, clusterName string) (bool, error) {
	exists, err := provider.CheckNodesExist(ctx, p, clusterName)
	if err != nil {
		return false, fmt.Errorf("check aks agent pools: %w", err)
	}

	return exists, nil
}

// DeleteNodes is a no-op for AKS: deleting the cluster deletes its agent
// pools atomically, so pool deletion is owned by the provisioner's cluster
// deletion — mirroring the gcp and AWS providers' contract.
func (p *Provider) DeleteNodes(_ context.Context, _ string) error {
	return nil
}

// GetClusterStatus aggregates agent-pool statuses into a
// provider.ClusterStatus. Returns provider.ErrClusterNotFound when the
// cluster does not exist. A cluster that exists but has no agent pools yields
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

	nodes := agentPoolInfos(cluster, clusterName)

	status := provider.BuildClusterStatus(nodes, agentPoolReadyState)
	if status == nil {
		status = &provider.ClusterStatus{Phase: provider.PhaseStopped, Nodes: nodes}
	}

	if cluster.Properties != nil {
		status.Endpoint = derefString(cluster.Properties.Fqdn)
	}

	return status, nil
}

// ResourceGroup returns the Azure resource group this provider was configured
// with (empty when operating subscription-wide).
func (p *Provider) ResourceGroup() string {
	return p.resourceGroup
}

// getCluster is the shared cluster fetch that guards against nil clients and
// translates client errors into the provider sentinels.
func (p *Provider) getCluster(
	ctx context.Context,
	clusterName string,
) (armcontainerservice.ManagedCluster, error) {
	if p.client == nil {
		return armcontainerservice.ManagedCluster{}, provider.ErrProviderUnavailable
	}

	resourceGroup, err := p.clusterResourceGroup(ctx, clusterName)
	if err != nil {
		return armcontainerservice.ManagedCluster{}, err
	}

	cluster, err := p.client.GetCluster(ctx, resourceGroup, clusterName)
	if err != nil {
		return armcontainerservice.ManagedCluster{}, translateClientErr(err)
	}

	return cluster, nil
}

// agentPoolsForScale is the shared prelude for StartNodes and StopNodes that
// fetches the cluster's agent pools (and the resource group they live in) and
// classifies an empty result as provider.ErrNoNodes.
func (p *Provider) agentPoolsForScale(
	ctx context.Context,
	clusterName string,
) ([]*armcontainerservice.ManagedClusterAgentPoolProfile, string, error) {
	if p.client == nil {
		return nil, "", provider.ErrProviderUnavailable
	}

	resourceGroup, err := p.clusterResourceGroup(ctx, clusterName)
	if err != nil {
		return nil, "", err
	}

	cluster, err := p.client.GetCluster(ctx, resourceGroup, clusterName)
	if err != nil {
		return nil, "", translateClientErr(err)
	}

	pools := agentPools(cluster)
	if len(pools) == 0 {
		return nil, "", provider.ErrNoNodes
	}

	return pools, resourceGroup, nil
}

// clusterResourceGroup resolves the resource group a cluster-scoped call
// should target. With a configured resource group it is used directly; with
// an empty one the cluster's own resource group is parsed from its ARM ID via
// a subscription-wide list, since cluster-scoped AKS calls require one.
func (p *Provider) clusterResourceGroup(
	ctx context.Context,
	clusterName string,
) (string, error) {
	if p.resourceGroup != "" {
		return p.resourceGroup, nil
	}

	clusters, err := p.client.ListClusters(ctx, "")
	if err != nil {
		return "", translateClientErr(err)
	}

	for _, cluster := range clusters {
		if cluster == nil || derefString(cluster.Name) != clusterName {
			continue
		}

		resourceID, parseErr := arm.ParseResourceID(derefString(cluster.ID))
		if parseErr != nil {
			return "", fmt.Errorf(
				"parse ARM ID of cluster %s: %w", clusterName, parseErr,
			)
		}

		return resourceID.ResourceGroupName, nil
	}

	return "", fmt.Errorf("%w: %s", provider.ErrClusterNotFound, clusterName)
}

// agentPools unwraps a cluster's agent-pool profiles, tolerating the
// zero-value cluster (nil Properties) the API never returns but tests may.
func agentPools(
	cluster armcontainerservice.ManagedCluster,
) []*armcontainerservice.ManagedClusterAgentPoolProfile {
	if cluster.Properties == nil {
		return nil
	}

	return cluster.Properties.AgentPoolProfiles
}

// agentPoolInfos maps a cluster's agent pools to NodeInfo entries.
func agentPoolInfos(
	cluster armcontainerservice.ManagedCluster,
	clusterName string,
) []provider.NodeInfo {
	pools := agentPools(cluster)
	nodes := make([]provider.NodeInfo, 0, len(pools))

	for _, pool := range pools {
		if pool == nil {
			continue
		}

		nodes = append(nodes, provider.NodeInfo{
			Name:        derefString(pool.Name),
			ClusterName: clusterName,
			Role:        nodeRoleWorker,
			State:       derefString(pool.ProvisioningState),
			ServerType:  derefString(pool.VMSize),
		})
	}

	return nodes
}

// translateClientErr maps AKS client errors onto the provider sentinels: an
// ARM 404 anywhere in the chain becomes provider.ErrClusterNotFound.
func translateClientErr(err error) error {
	if err == nil {
		return nil
	}

	var responseErr *azcore.ResponseError
	if errors.As(err, &responseErr) && responseErr.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s", provider.ErrClusterNotFound, strings.TrimSpace(err.Error()))
	}

	return err
}

// derefString unwraps the SDK's optional string fields, mapping nil to "".
func derefString(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

// derefInt32 unwraps the SDK's optional int32 fields, mapping nil to zero.
func derefInt32(value *int32) int32 {
	if value == nil {
		return 0
	}

	return *value
}
