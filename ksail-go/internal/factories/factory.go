package factory

import (
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/internal/loader"
	"github.com/devantler-tech/ksail/internal/utils"
	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
	reconboot "github.com/devantler-tech/ksail/pkg/bootstrapper/reconciliation_tool"
	clusterprovisioner "github.com/devantler-tech/ksail/pkg/provisioner/cluster"
	containerengineprovisioner "github.com/devantler-tech/ksail/pkg/provisioner/container_engine"
)

func ClusterProvisioner(ksailConfig *ksailcluster.Cluster) (clusterprovisioner.ClusterProvisioner, error) {
	if ksailConfig.Spec.ContainerEngine == ksailcluster.ContainerEnginePodman {
		podmanSock := fmt.Sprintf("unix:///run/user/%d/podman/podman.sock", os.Getuid())
		os.Setenv("DOCKER_HOST", podmanSock)
	}
	var provisioner clusterprovisioner.ClusterProvisioner
	switch ksailConfig.Spec.Distribution {
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
		return nil, fmt.Errorf("unsupported distribution '%s'", ksailConfig.Spec.Distribution)
	}
	return provisioner, nil
}

func ContainerEngineProvisioner(ksailConfig *ksailcluster.Cluster) (containerengineprovisioner.ContainerEngineProvisioner, error) {
	switch ksailConfig.Spec.ContainerEngine {
	case ksailcluster.ContainerEngineDocker:
		return containerengineprovisioner.NewDockerProvisioner(ksailConfig), nil
	case ksailcluster.ContainerEnginePodman:
		return containerengineprovisioner.NewPodmanProvisioner(ksailConfig), nil
	default:
		return nil, fmt.Errorf("unsupported container engine '%s'", ksailConfig.Spec.ContainerEngine)
	}
}

func ReconciliationTool(reconciliationTool ksailcluster.ReconciliationTool, ksailConfig *ksailcluster.Cluster) (reconboot.Bootstrapper, error) {
	var reconciliationToolBootstrapper reconboot.Bootstrapper
	switch reconciliationTool {
	case ksailcluster.ReconciliationToolKubectl:
		// Bootstrap with kubectl
	case ksailcluster.ReconciliationToolFlux:
		kubeconfigPath, err := utils.ExpandPath(ksailConfig.Spec.Connection.Kubeconfig)
		if err != nil {
			return nil, err
		}
		reconciliationToolBootstrapper = reconboot.NewFluxBootstrapper(
			kubeconfigPath,
			ksailConfig.Spec.Connection.Context,
			ksailConfig.Spec.Connection.Timeout.Duration,
		)
	case ksailcluster.ReconciliationToolArgoCD:
		// Bootstrap with ArgoCD
	default:
		return nil, fmt.Errorf("unsupported reconciliation tool '%s'", reconciliationTool)
	}
	return reconciliationToolBootstrapper, nil
}
