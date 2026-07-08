package azure

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
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
// dependency to the calls it makes instead.
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
	// StartCluster starts a stopped managed cluster.
	StartCluster(ctx context.Context, resourceGroup, name string) error
	// StopCluster stops a running managed cluster.
	StopCluster(ctx context.Context, resourceGroup, name string) error
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

// StartNodes starts a stopped cluster via AKS's native start, the
// counterpart of [Provider.StopNodes]. A cluster already running is left
// alone, keeping the call idempotent (ARM rejects starting a running
// cluster).
func (p *Provider) StartNodes(ctx context.Context, clusterName string) error {
	resourceGroup, cluster, err := p.clusterWithGroup(ctx, clusterName)
	if err != nil {
		return err
	}

	if powerCode(cluster) == armcontainerservice.CodeRunning {
		return nil
	}

	err = p.client.StartCluster(ctx, resourceGroup, clusterName)
	if err != nil {
		return fmt.Errorf("start cluster %s: %w", clusterName, err)
	}

	return nil
}

// StopNodes stops the cluster via AKS's native stop — Azure's supported way
// to halt node costs. Unlike the gcp provider's pool-resize stop, agent pools
// cannot be resized to zero on AKS (system pools require at least one node),
// so the whole cluster — control plane included — deallocates while its state
// and configuration are kept. A cluster already stopped is left alone,
// keeping the call idempotent (ARM rejects stopping a stopped cluster).
func (p *Provider) StopNodes(ctx context.Context, clusterName string) error {
	resourceGroup, cluster, err := p.clusterWithGroup(ctx, clusterName)
	if err != nil {
		return err
	}

	if powerCode(cluster) == armcontainerservice.CodeStopped {
		return nil
	}

	err = p.client.StopCluster(ctx, resourceGroup, clusterName)
	if err != nil {
		return fmt.Errorf("stop cluster %s: %w", clusterName, err)
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
	_, nodes, err := p.clusterNodes(ctx, clusterName)

	return nodes, err
}

// ListAllClusters returns the names of all AKS clusters in the configured
// resource group (the whole subscription when the provider was built with an
// empty resource group).
func (p *Provider) ListAllClusters(ctx context.Context) ([]string, error) {
	clusters, err := provider.FetchOrTranslate(
		p.client != nil,
		func() ([]*armcontainerservice.ManagedCluster, error) {
			return p.client.ListClusters(ctx, p.resourceGroup)
		},
		translateClientErr,
	)
	if err != nil {
		return nil, err //nolint:wrapcheck // FetchOrTranslate already ran the error through translateClientErr
	}

	return provider.NamesFrom(clusters, func(c *armcontainerservice.ManagedCluster) string {
		return derefString(c.Name)
	}), nil
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
	cluster, nodes, err := p.clusterNodes(ctx, clusterName)
	if err != nil {
		return nil, err
	}

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

// clusterNodes is the shared fetch-and-collapse behind ListNodes and
// GetClusterStatus: it fetches the cluster once and maps its agent pools to
// NodeInfo entries, returning the cluster too for callers that need more of
// it (e.g. the API-server FQDN).
func (p *Provider) clusterNodes(
	ctx context.Context,
	clusterName string,
) (armcontainerservice.ManagedCluster, []provider.NodeInfo, error) {
	_, cluster, err := p.clusterWithGroup(ctx, clusterName)
	if err != nil {
		return armcontainerservice.ManagedCluster{}, nil, err
	}

	return cluster, agentPoolInfos(cluster, clusterName), nil
}

// clusterWithGroup is the shared cluster fetch that guards against nil
// clients, resolves the resource group the cluster lives in, and translates
// client errors into the provider sentinels — the prelude of every
// cluster-scoped operation.
func (p *Provider) clusterWithGroup(
	ctx context.Context,
	clusterName string,
) (string, armcontainerservice.ManagedCluster, error) {
	if p.client == nil {
		return "", armcontainerservice.ManagedCluster{}, provider.ErrProviderUnavailable
	}

	resourceGroup, err := p.clusterResourceGroup(ctx, clusterName)
	if err != nil {
		return "", armcontainerservice.ManagedCluster{}, err
	}

	cluster, err := p.client.GetCluster(ctx, resourceGroup, clusterName)
	if err != nil {
		return "", armcontainerservice.ManagedCluster{}, translateClientErr(err)
	}

	return resourceGroup, cluster, nil
}

// clusterResourceGroup resolves the resource group a cluster-scoped call
// should target. With a configured resource group it is used directly; with
// an empty one the cluster's own resource group is parsed from its ARM ID via
// a subscription-wide list, since cluster-scoped AKS calls require one.
func (p *Provider) clusterResourceGroup(
	ctx context.Context,
	clusterName string,
) (string, error) {
	group, err := aks.ResolveClusterResourceGroup(
		ctx, p.client, clusterName, p.resourceGroup, provider.ErrClusterNotFound,
	)
	if err != nil {
		return "", translateClientErr(err)
	}

	return group, nil
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

// powerCode unwraps a cluster's power state, mapping any missing level of
// the optional chain to the empty code (neither running nor stopped, so the
// idempotence short-circuits never fire on it and the native call decides).
func powerCode(cluster armcontainerservice.ManagedCluster) armcontainerservice.Code {
	if cluster.Properties == nil || cluster.Properties.PowerState == nil ||
		cluster.Properties.PowerState.Code == nil {
		return ""
	}

	return *cluster.Properties.PowerState.Code
}

// derefString unwraps the SDK's optional string fields, mapping nil to "".
func derefString(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}
