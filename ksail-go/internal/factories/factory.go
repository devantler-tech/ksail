package factory

import (
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/internal/loader"
	"github.com/devantler-tech/ksail/internal/utils"
	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
	reconciliationtoolbootstrapper "github.com/devantler-tech/ksail/pkg/bootstrapper/reconciliation_tool"
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

func ContainerEngineProvisioner(cfg *ksailcluster.Cluster) (containerengineprovisioner.ContainerEngineProvisioner, error) {
	switch cfg.Spec.ContainerEngine {
	case ksailcluster.ContainerEngineDocker:
		return containerengineprovisioner.NewDockerProvisioner(cfg), nil
	case ksailcluster.ContainerEnginePodman:
		return containerengineprovisioner.NewPodmanProvisioner(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported container engine '%s'", cfg.Spec.ContainerEngine)
	}
}

func ReconciliationTool(cfg *ksailcluster.Cluster) (reconciliationtoolbootstrapper.Bootstrapper, error) {
	kubeconfigPath, err := utils.ExpandPath(cfg.Spec.Connection.Kubeconfig)
	if err != nil {
		return nil, err
	}
	var reconciliationToolBootstrapper reconciliationtoolbootstrapper.Bootstrapper
	switch cfg.Spec.ReconciliationTool {
	case ksailcluster.ReconciliationToolKubectl:
		reconciliationToolBootstrapper = reconciliationtoolbootstrapper.NewKubectlBootstrapper(
			kubeconfigPath,
			cfg.Spec.Connection.Context,
			cfg.Spec.Connection.Timeout.Duration,
		)
	case ksailcluster.ReconciliationToolFlux:
		reconciliationToolBootstrapper = reconciliationtoolbootstrapper.NewFluxBootstrapper(
			kubeconfigPath,
			cfg.Spec.Connection.Context,
			cfg.Spec.Connection.Timeout.Duration,
		)
	case ksailcluster.ReconciliationToolArgoCD:
	default:
		return nil, fmt.Errorf("unsupported reconciliation tool '%s'", cfg.Spec.ReconciliationTool)
	}
	return reconciliationToolBootstrapper, nil
}
