package clusterprovisioner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/kind"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	talosindickerprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talosindocker"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// ErrUnsupportedDistribution is returned when an unsupported distribution is specified.
var ErrUnsupportedDistribution = errors.New("unsupported distribution")

const defaultKubeconfigPath = "~/.kube/config"

// Factory creates distribution-specific cluster provisioners based on the KSail cluster configuration.
type Factory interface {
	Create(ctx context.Context, cluster *v1alpha1.Cluster) (ClusterProvisioner, any, error)
}

// DefaultFactory implements Factory using the existing CreateClusterProvisioner helper.
type DefaultFactory struct{}

// Create selects the correct distribution provisioner for the KSail cluster configuration.
func (DefaultFactory) Create(
	_ context.Context,
	cluster *v1alpha1.Cluster,
) (ClusterProvisioner, any, error) {
	if cluster == nil {
		return nil, nil, fmt.Errorf(
			"cluster configuration is required: %w",
			ErrUnsupportedDistribution,
		)
	}

	switch cluster.Spec.Cluster.Distribution {
	case v1alpha1.DistributionKind:
		return createKindProvisioner(
			cluster.Spec.Cluster.DistributionConfig,
			cluster.Spec.Cluster.Connection.Kubeconfig,
		)
	case v1alpha1.DistributionK3d:
		return createK3dProvisioner(
			cluster.Spec.Cluster.DistributionConfig,
		)
	case v1alpha1.DistributionTalosInDocker:
		// Derive cluster name from context or use default
		clusterName := strings.TrimSpace(cluster.Spec.Cluster.Connection.Context)
		if clusterName == "" {
			clusterName = talosconfigmanager.DefaultClusterName
		}

		return createTalosInDockerProvisioner(
			cluster.Spec.Cluster.DistributionConfig,
			cluster.Spec.Cluster.Connection.Kubeconfig,
			clusterName,
			cluster.Spec.Cluster.Options.TalosInDocker,
			cluster.Spec.Cluster.CNI,
			nil, // Mirror registries from scaffolded patches; runtime mirrors not supported here
		)
	default:
		return nil, "", fmt.Errorf(
			"%w: %s",
			ErrUnsupportedDistribution,
			cluster.Spec.Cluster.Distribution,
		)
	}
}

func createKindProvisioner(
	distributionConfigPath string,
	kubeconfigPath string,
) (*kindprovisioner.KindClusterProvisioner, *v1alpha4.Cluster, error) {
	kindConfigMgr := kindconfigmanager.NewConfigManager(distributionConfigPath)

	kindConfig, err := kindConfigMgr.LoadConfig(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load Kind configuration: %w", err)
	}

	provisioner, err := createKindProvisionerFromConfig(kindConfig, kubeconfigPath)
	if err != nil {
		return nil, nil, err
	}

	return provisioner, kindConfig, nil
}

func createKindProvisionerFromConfig(
	kindConfig *v1alpha4.Cluster,
	kubeconfigPath string,
) (*kindprovisioner.KindClusterProvisioner, error) {
	provider := kindprovisioner.NewDefaultKindProviderAdapter()

	dockerClient, err := kindprovisioner.NewDefaultDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	if kubeconfigPath == "" {
		kubeconfigPath = defaultKubeconfigPath
	}

	return kindprovisioner.NewKindClusterProvisioner(
		kindConfig,
		kubeconfigPath,
		provider,
		dockerClient,
	), nil
}

func createK3dProvisioner(
	distributionConfigPath string,
) (*k3dprovisioner.K3dClusterProvisioner, *k3dv1alpha5.SimpleConfig, error) {
	k3dConfigMgr := k3dconfigmanager.NewConfigManager(distributionConfigPath)

	k3dConfig, err := k3dConfigMgr.LoadConfig(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load K3d configuration: %w", err)
	}

	provisioner := k3dprovisioner.NewK3dClusterProvisioner(
		k3dConfig,
		distributionConfigPath,
	)

	return provisioner, k3dConfig, nil
}

// buildTalosCNIPatches creates runtime patches for CNI configuration.
func buildTalosCNIPatches(cni v1alpha1.CNI) []talosconfigmanager.Patch {
	var runtimePatches []talosconfigmanager.Patch

	// Add CNI disable patch if using any non-default CNI (e.g., Cilium, Calico, None)
	// Empty string is treated as default CNI (for imperative mode without config file)
	if cni != v1alpha1.CNIDefault && cni != "" {
		runtimePatches = append(runtimePatches, talosconfigmanager.Patch{
			Path:  "in-memory:disable-default-cni",
			Scope: talosconfigmanager.PatchScopeCluster,
			Content: []byte(`cluster:
  network:
    cni:
      name: none
`),
		})
	}

	return runtimePatches
}

func applyMirrorRegistriesToTalosConfig(
	talosConfigs *talosconfigmanager.Configs,
	mirrorRegistries []string,
) error {
	if len(mirrorRegistries) == 0 {
		return nil
	}

	mirrorSpecs := registry.ParseMirrorSpecs(mirrorRegistries)
	mirrors := make([]talosconfigmanager.MirrorRegistry, 0, len(mirrorSpecs))

	for _, spec := range mirrorSpecs {
		if spec.Host == "" {
			continue
		}

		mirrors = append(mirrors, talosconfigmanager.MirrorRegistry{
			Host:      spec.Host,
			Endpoints: []string{"http://" + spec.Host + ":5000"},
		})
	}

	err := talosConfigs.ApplyMirrorRegistries(mirrors)
	if err != nil {
		return fmt.Errorf("failed to apply mirror registries to Talos config: %w", err)
	}

	return nil
}

func createTalosInDockerProvisioner(
	distributionConfigPath string,
	kubeconfigPath string,
	clusterName string,
	opts v1alpha1.OptionsTalosInDocker,
	cni v1alpha1.CNI,
	mirrorRegistries []string,
) (*talosindickerprovisioner.TalosInDockerProvisioner, *talosconfigmanager.Configs, error) {
	// Load talos config with CNI patches applied
	manager := talosconfigmanager.NewConfigManager(
		distributionConfigPath,
		clusterName,
		"", // Use default Kubernetes version
		"", // Use default network CIDR
	).WithAdditionalPatches(buildTalosCNIPatches(cni))

	talosConfigs, err := manager.LoadConfig(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load Talos configuration: %w", err)
	}

	err = applyMirrorRegistriesToTalosConfig(talosConfigs, mirrorRegistries)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to apply mirror registries: %w", err)
	}

	// Create options and apply configured node counts
	options := talosindickerprovisioner.NewOptions().WithKubeconfigPath(kubeconfigPath)
	if opts.ControlPlanes > 0 {
		options.WithControlPlaneNodes(int(opts.ControlPlanes))
	}

	if opts.Workers > 0 {
		options.WithWorkerNodes(int(opts.Workers))
	}

	// Create provisioner with loaded configs and options
	provisioner := talosindickerprovisioner.NewTalosInDockerProvisioner(talosConfigs, options)

	dockerClient, err := kindprovisioner.NewDefaultDockerClient()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	provisioner.WithDockerClient(dockerClient)

	return provisioner, talosConfigs, nil
}
