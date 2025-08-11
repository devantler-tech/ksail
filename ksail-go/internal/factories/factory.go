package factory

import (
	"fmt"

	"github.com/devantler-tech/ksail/internal/loader"
	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
	reconboot "github.com/devantler-tech/ksail/pkg/bootstrapper/reconciliation_tool"
	clusterprovisioner "github.com/devantler-tech/ksail/pkg/provisioner/cluster"
)

func Provisioner(distribution ksailcluster.Distribution, ksailConfig *ksailcluster.Cluster) (clusterprovisioner.ClusterProvisioner, error) {
	var provisioner clusterprovisioner.ClusterProvisioner
	switch distribution {
	case ksailcluster.DistributionKind:
		kindConfig, err := loader.NewKindConfigLoader().Load()
		if err != nil {
			return nil, err
		}
		provisioner = clusterprovisioner.NewKindClusterProvisioner(ksailConfig, &kindConfig)
	case ksailcluster.DistributionK3d:
		k3dConfig, err := loader.NewK3dConfigLoader().Load()
		if err != nil {
			return nil, err
		}
		provisioner = clusterprovisioner.NewK3dClusterProvisioner(ksailConfig, &k3dConfig)
	default:
		return nil, fmt.Errorf("unsupported distribution '%s'", distribution)
	}
	return provisioner, nil
}

func ReconciliationTool(reconciliationTool ksailcluster.ReconciliationTool, ksailConfig *ksailcluster.Cluster) (reconboot.Bootstrapper, error) {
	var reconciliationToolBootstrapper reconboot.Bootstrapper
	switch reconciliationTool {
	case ksailcluster.ReconciliationToolKubectl:
		// Bootstrap with kubectl
	case ksailcluster.ReconciliationToolFlux:
		reconciliationToolBootstrapper = reconboot.NewFluxOperatorBootstrapper(
			ksailConfig.Spec.Connection.Kubeconfig,
			ksailConfig.Spec.Connection.Context,
		)
	case ksailcluster.ReconciliationToolArgoCD:
		// Bootstrap with ArgoCD
	default:
		return nil, fmt.Errorf("unsupported reconciliation tool '%s'", reconciliationTool)
	}
	return reconciliationToolBootstrapper, nil
}
