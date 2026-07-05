package aksprovisioner

import (
	"context"
	"fmt"
	"slices"

	armcontainerservice "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
	"github.com/devantler-tech/ksail/v7/pkg/client/aks"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
)

// ClusterClient is the slice of the pkg/client/aks surface the provisioner
// drives. It is the provisioner's test seam: the aks client has no injectable
// manager (its own tests fake the ARM transport), so the provisioner narrows
// the dependency to the calls it makes instead — the same trade
// pkg/svc/provider/azure makes.
type ClusterClient interface {
	// CreateCluster creates (or updates) the named managed cluster and blocks
	// until the operation completes.
	CreateCluster(
		ctx context.Context,
		resourceGroup, name string,
		cluster armcontainerservice.ManagedCluster,
	) (armcontainerservice.ManagedCluster, error)
	// DeleteCluster deletes the named managed cluster and blocks until the
	// operation completes.
	DeleteCluster(ctx context.Context, resourceGroup, name string) error
	// ListClusters lists the clusters in a resource group, or across the whole
	// subscription when resourceGroup is empty.
	ListClusters(
		ctx context.Context,
		resourceGroup string,
	) ([]*armcontainerservice.ManagedCluster, error)
	// GetCluster fetches the named managed cluster's definition.
	GetCluster(
		ctx context.Context,
		resourceGroup, name string,
	) (armcontainerservice.ManagedCluster, error)
	// GetClusterUserCredentials fetches the named managed cluster's user
	// kubeconfig as served by ARM.
	GetClusterUserCredentials(
		ctx context.Context,
		resourceGroup, name string,
	) ([]byte, error)
}

// staticClusterClientCheck asserts at compile time that the production aks
// client satisfies the provisioner's seam.
var _ ClusterClient = (*aks.Client)(nil)

// Provisioner manages Azure Kubernetes Service clusters via the native Go
// SDK.
//
// Cluster lifecycle operations delegate to pkg/client/aks (which hides the
// ARM request shapes and long-running-operation polling); Start/Stop delegate
// to the Azure infrastructure provider, which drives AKS's native
// managed-cluster start/stop. This mirrors the GKE provisioner, with the
// resource group playing the role GKE's location plays: empty means "not
// pinned" — reads resolve the cluster's own resource group from its ARM ID
// via a subscription-wide list, while Create requires a concrete value.
type Provisioner struct {
	// name is the cluster name derived from the ksail.yaml.
	name string
	// resourceGroup is the Azure resource group cluster-scoped calls target.
	// Empty means "not pinned": reads resolve the cluster's own resource
	// group via a subscription-wide list, while Create — which has nothing to
	// resolve from — requires a concrete value. The subscription itself is
	// baked into the AKS client.
	resourceGroup string
	// clusterSpec is the declarative cluster specification submitted by
	// Create. Required for Create; optional for inspection-only use.
	clusterSpec *armcontainerservice.ManagedCluster
	// client is the AKS SDK wrapper, narrowed to the provisioner's seam.
	client ClusterClient
	// infraProvider is the Azure provider used for Start/Stop semantics
	// (native managed-cluster start/stop). Optional: if nil, Start/Stop
	// return an error.
	infraProvider provider.Provider
}

// NewProvisioner builds a Provisioner. The AKS client must be non-nil;
// clusterSpec and a concrete resource group are required for Create but
// optional for inspection-only use.
func NewProvisioner(
	name, resourceGroup string,
	clusterSpec *armcontainerservice.ManagedCluster,
	client ClusterClient,
	infraProvider provider.Provider,
) (*Provisioner, error) {
	if client == nil {
		return nil, ErrClientRequired
	}

	return &Provisioner{
		name:          name,
		resourceGroup: resourceGroup,
		clusterSpec:   clusterSpec,
		client:        client,
		infraProvider: infraProvider,
	}, nil
}

// SetProvider sets the infrastructure provider used by Start/Stop.
func (p *Provisioner) SetProvider(prov provider.Provider) {
	p.infraProvider = prov
}

// Create provisions a new AKS cluster from the declarative cluster spec and
// blocks until the create operation completes.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	if p.clusterSpec == nil {
		return ErrClusterSpecRequired
	}

	if p.resourceGroup == "" {
		return ErrResourceGroupRequired
	}

	target := p.resolveName(name)
	if target == "" {
		return ErrNameRequired
	}

	_, err := p.client.CreateCluster(ctx, p.resourceGroup, target, *p.clusterSpec)
	if err != nil {
		return fmt.Errorf("aks create cluster: %w", err)
	}

	return nil
}

// Delete tears down the AKS cluster and blocks until the delete operation
// completes. When no resource group is pinned, the cluster's own resource
// group is resolved from its ARM ID via a subscription-wide list first.
func (p *Provisioner) Delete(ctx context.Context, name string) error {
	target := p.resolveName(name)
	if target == "" {
		return fmt.Errorf("%w: no cluster name configured", ErrClusterNotFound)
	}

	resourceGroup, err := p.resolveResourceGroup(ctx, target)
	if err != nil {
		return err
	}

	err = p.client.DeleteCluster(ctx, resourceGroup, target)
	if err != nil {
		return fmt.Errorf("aks delete cluster: %w", err)
	}

	return nil
}

// Start resumes a stopped AKS cluster via Azure's native managed-cluster
// start, restoring the control plane and every agent pool.
func (p *Provisioner) Start(ctx context.Context, name string) error {
	if p.infraProvider == nil {
		return fmt.Errorf("%w: start requires an Azure provider", clustererr.ErrUnsupportedProvider)
	}

	target := p.resolveName(name)

	err := p.infraProvider.StartNodes(ctx, target)
	if err != nil {
		return fmt.Errorf("start nodes: %w", err)
	}

	return nil
}

// Stop stops the AKS cluster via Azure's native managed-cluster stop — the
// whole cluster, control plane included, deallocates while its state and
// configuration are kept (agent pools cannot be scaled to zero on AKS; system
// pools require at least one node).
func (p *Provisioner) Stop(ctx context.Context, name string) error {
	if p.infraProvider == nil {
		return fmt.Errorf("%w: stop requires an Azure provider", clustererr.ErrUnsupportedProvider)
	}

	target := p.resolveName(name)

	err := p.infraProvider.StopNodes(ctx, target)
	if err != nil {
		return fmt.Errorf("stop nodes: %w", err)
	}

	return nil
}

// List returns the names of every AKS cluster in the configured resource
// group, or across the whole subscription when no resource group is pinned.
func (p *Provisioner) List(ctx context.Context) ([]string, error) {
	clusters, err := p.client.ListClusters(ctx, p.resourceGroup)
	if err != nil {
		return nil, fmt.Errorf("aks list clusters: %w", err)
	}

	names := make([]string, 0, len(clusters))

	for _, cluster := range clusters {
		if cluster == nil || cluster.Name == nil {
			continue
		}

		names = append(names, *cluster.Name)
	}

	return names, nil
}

// Exists reports whether a cluster with the given name (or the provisioner
// default) exists, as membership in [Provisioner.List] — GetCluster reports a
// missing cluster as an ARM ResourceNotFound error, which is harder to
// classify reliably than an empty list result (the same trade the GKE and
// EKS provisioners make).
func (p *Provisioner) Exists(ctx context.Context, name string) (bool, error) {
	target := p.resolveName(name)
	if target == "" {
		return false, nil
	}

	names, err := p.List(ctx)
	if err != nil {
		return false, err
	}

	return slices.Contains(names, target), nil
}

// resolveName returns the caller-supplied name when set, otherwise falls
// back to the provisioner's configured name.
func (p *Provisioner) resolveName(name string) string {
	if name != "" {
		return name
	}

	return p.name
}

// resolveResourceGroup returns the pinned resource group when set; otherwise
// it resolves the named cluster's own resource group from its ARM ID via a
// subscription-wide list, mirroring the Azure provider's resolution for
// cluster-scoped calls.
func (p *Provisioner) resolveResourceGroup(
	ctx context.Context,
	clusterName string,
) (string, error) {
	group, err := aks.ResolveClusterResourceGroup(
		ctx, p.client, clusterName, p.resourceGroup, ErrClusterNotFound,
	)
	if err != nil {
		return "", fmt.Errorf("resolve resource group of %s: %w", clusterName, err)
	}

	return group, nil
}
