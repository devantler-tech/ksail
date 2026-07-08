package aksprovisioner

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
)

// provisioningStateSucceeded is the ARM provisioning state of a managed
// cluster whose control plane is fully provisioned — the AKS counterpart to
// the GKE connector's RUNNING gate and the EKS connector's ACTIVE gate.
const provisioningStateSucceeded = "Succeeded"

// Kubeconfig implements the clusterprovisioner.Connector capability for an
// AKS cluster. Unlike GKE/EKS — where the connector assembles a kubeconfig
// from the control-plane endpoint, CA, and a freshly minted bearer token —
// ARM serves a ready-made kubeconfig via ListClusterUserCredentials, with the
// server set to the cluster's FQDN (reachable from wherever the operator
// runs, no address rewrite needed) and, for clusters with local accounts, an
// embedded client certificate that authenticates without ambient Azure
// credentials. AAD-only clusters (local accounts disabled) instead get an
// exec-plugin kubeconfig that needs kubelogin, which the operator image does
// not carry — a documented limitation of this increment. It returns
// clustererr.ErrKubeconfigNotReady until the cluster's provisioning state is
// Succeeded so the caller requeues.
//
//nolint:wrapcheck // WithResolvedTarget already labels the error; nothing to add here.
func (p *Provisioner) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	return clustererr.WithResolvedTarget(
		ctx,
		p.resolveName,
		name,
		ErrClusterNotFound,
		p.kubeconfigForTarget,
	)
}

// kubeconfigForTarget fetches the AKS kubeconfig for an already-resolved target name.
func (p *Provisioner) kubeconfigForTarget(ctx context.Context, target string) ([]byte, error) {
	resourceGroup, err := p.resolveResourceGroup(ctx, target)
	if err != nil {
		return nil, err
	}

	cluster, err := p.client.GetCluster(ctx, resourceGroup, target)
	if err != nil {
		return nil, fmt.Errorf("aks get cluster: %w", err)
	}

	if state := provisioningState(cluster); state != provisioningStateSucceeded {
		return nil, fmt.Errorf(
			"%w: aks cluster %q is %s",
			clustererr.ErrKubeconfigNotReady, target, state,
		)
	}

	raw, err := p.client.GetClusterUserCredentials(ctx, resourceGroup, target)
	if err != nil {
		return nil, fmt.Errorf("aks cluster user credentials: %w", err)
	}

	return raw, nil
}

// provisioningState extracts the cluster's ARM provisioning state, reading
// through the nullable properties chain; an absent state reads as "unknown"
// so the not-ready error names something actionable.
func provisioningState(cluster armcontainerservice.ManagedCluster) string {
	if cluster.Properties == nil || cluster.Properties.ProvisioningState == nil {
		return "unknown"
	}

	return *cluster.Properties.ProvisioningState
}
