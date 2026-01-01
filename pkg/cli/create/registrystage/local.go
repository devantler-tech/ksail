package registrystage

import (
	"context"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/k3d"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// LocalRegistryRole represents the type of local registry stage operation.
type LocalRegistryRole int

const (
	// LocalRegistryProvision creates the local registry before cluster creation.
	LocalRegistryProvision LocalRegistryRole = iota
	// LocalRegistryConnect attaches the local registry after cluster creation.
	LocalRegistryConnect
)

// LocalRegistryInfo contains display information for a local registry stage.
type LocalRegistryInfo struct {
	Title         string
	Emoji         string
	Activity      string
	Success       string
	FailurePrefix string
}

// ToInfo converts LocalRegistryInfo to the generic Info type.
func (l LocalRegistryInfo) ToInfo() Info {
	return Info(l)
}

// LocalRegistryContext contains the configuration needed for local registry operations.
type LocalRegistryContext struct {
	ClusterName string
	NetworkName string
}

// LocalRegistryAction is a function that performs an operation on the local registry.
type LocalRegistryAction func(context.Context, registry.Service, LocalRegistryContext) error

// LocalRegistryServiceFactory creates a registry service from a config.
type LocalRegistryServiceFactory func(registry.Config) (registry.Service, error)

// LocalRegistryDependencies holds injectable dependencies for local registry operations.
type LocalRegistryDependencies struct {
	ServiceFactory LocalRegistryServiceFactory
}

// ProvisionLocalRegistryInfo returns the stage info for local registry provisioning.
func ProvisionLocalRegistryInfo() LocalRegistryInfo {
	return LocalRegistryInfo{
		Title:         "Create local registry...",
		Emoji:         "🗄️",
		Activity:      "creating local registry",
		Success:       "local registry created",
		FailurePrefix: "failed to create local registry",
	}
}

// ConnectLocalRegistryInfo returns the stage info for local registry connection.
func ConnectLocalRegistryInfo() LocalRegistryInfo {
	return LocalRegistryInfo{
		Title:         "Attach local registry...",
		Emoji:         "🔌",
		Activity:      "attaching local registry to cluster",
		Success:       "local registry attached to cluster",
		FailurePrefix: "failed to attach local registry",
	}
}

// CleanupLocalRegistryInfo returns the stage info for local registry cleanup.
func CleanupLocalRegistryInfo() LocalRegistryInfo {
	return LocalRegistryInfo{
		Title:         "Delete local registry...",
		Emoji:         "🧹",
		Activity:      "deleting local registry",
		Success:       "local registry deleted",
		FailurePrefix: "failed to delete local registry",
	}
}

// DefaultLocalRegistryDependencies returns default dependencies for local registry operations.
func DefaultLocalRegistryDependencies() LocalRegistryDependencies {
	return LocalRegistryDependencies{
		ServiceFactory: registry.NewService,
	}
}

// NewLocalRegistryContext creates a local registry context from configuration.
func NewLocalRegistryContext(
	clusterCfg *v1alpha1.Cluster,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
) LocalRegistryContext {
	clusterName := ResolveLocalRegistryClusterName(clusterCfg, kindConfig, k3dConfig, talosConfig)
	networkName := ResolveLocalRegistryNetworkName(clusterCfg, clusterName)

	return LocalRegistryContext{
		ClusterName: clusterName,
		NetworkName: networkName,
	}
}

// ResolveLocalRegistryClusterName determines the cluster name for registry operations.
func ResolveLocalRegistryClusterName(
	clusterCfg *v1alpha1.Cluster,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
) string {
	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionKind:
		if kindConfig != nil {
			if name := strings.TrimSpace(kindConfig.Name); name != "" {
				return name
			}
		}
	case v1alpha1.DistributionK3d:
		return k3dconfigmanager.ResolveClusterName(clusterCfg, k3dConfig)
	case v1alpha1.DistributionTalos:
		// Talos uses talos config name if available, falls back to Connection.Context
		if talosConfig != nil && talosConfig.Name != "" {
			return talosConfig.Name
		}

		if name := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Context); name != "" {
			return name
		}

		return "talos-default"
	}

	if name := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Context); name != "" {
		return name
	}

	return "ksail"
}

// ResolveLocalRegistryNetworkName determines the Docker network name for the local registry.
func ResolveLocalRegistryNetworkName(
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) string {
	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionKind:
		return "kind"
	case v1alpha1.DistributionK3d:
		trimmed := strings.TrimSpace(clusterName)
		if trimmed == "" {
			trimmed = "k3d"
		}

		return "k3d-" + trimmed
	case v1alpha1.DistributionTalos:
		// Talos uses cluster name as Docker network name
		trimmed := strings.TrimSpace(clusterName)
		if trimmed == "" {
			trimmed = "talos-default"
		}

		return trimmed
	default:
		return ""
	}
}

// ProvisionLocalRegistry creates and starts the local registry.
func ProvisionLocalRegistry(clusterCfg *v1alpha1.Cluster) LocalRegistryAction {
	return func(execCtx context.Context, svc registry.Service, ctx LocalRegistryContext) error {
		createOpts := newLocalRegistryCreateOptions(clusterCfg, ctx)

		_, createErr := svc.Create(execCtx, createOpts)
		if createErr != nil {
			return fmt.Errorf("create local registry: %w", createErr)
		}

		_, startErr := svc.Start(execCtx, registry.StartOptions{Name: createOpts.Name})
		if startErr != nil {
			return fmt.Errorf("start local registry: %w", startErr)
		}

		return nil
	}
}

// ConnectLocalRegistry attaches the local registry to the cluster network.
func ConnectLocalRegistry() LocalRegistryAction {
	return func(execCtx context.Context, svc registry.Service, ctx LocalRegistryContext) error {
		startOpts := registry.StartOptions{
			Name:        buildLocalRegistryName(),
			NetworkName: ctx.NetworkName,
		}

		_, err := svc.Start(execCtx, startOpts)
		if err != nil {
			return fmt.Errorf("attach local registry: %w", err)
		}

		return nil
	}
}

// CleanupLocalRegistry stops and removes the local registry.
func CleanupLocalRegistry(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	deleteVolumes bool,
	dependencies LocalRegistryDependencies,
	dockerInvoker DockerClientInvoker,
) error {
	if clusterCfg.Spec.Cluster.LocalRegistry != v1alpha1.LocalRegistryEnabled {
		return nil
	}

	// Use cached distribution config from ConfigManager
	distConfig := cfgManager.DistributionConfig

	ctx := NewLocalRegistryContext(clusterCfg, distConfig.Kind, distConfig.K3d, distConfig.Talos)

	// Cleanup doesn't show title activity messages, so use a dummy tracker
	dummyTracker := true

	return runLocalRegistryStage(
		cmd,
		deps,
		CleanupLocalRegistryInfo(),
		func(execCtx context.Context, svc registry.Service) error {
			registryName := buildLocalRegistryName()
			volumeName := registryName

			if deleteVolumes {
				status, statusErr := svc.Status(execCtx, registry.StatusOptions{Name: registryName})
				if statusErr == nil && strings.TrimSpace(status.VolumeName) != "" {
					volumeName = status.VolumeName
				}
			}

			stopOpts := registry.StopOptions{
				Name:         registryName,
				ClusterName:  ctx.ClusterName,
				NetworkName:  ctx.NetworkName,
				DeleteVolume: deleteVolumes,
				VolumeName:   volumeName,
			}

			err := svc.Stop(execCtx, stopOpts)
			if err != nil {
				return fmt.Errorf("stop local registry: %w", err)
			}

			return nil
		},
		&dummyTracker,
		dependencies,
		dockerInvoker,
	)
}

// RunLocalRegistryAction executes a local registry action with proper lifecycle management.
func RunLocalRegistryAction(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	info LocalRegistryInfo,
	action LocalRegistryAction,
	firstActivityShown *bool,
	dependencies LocalRegistryDependencies,
	dockerInvoker DockerClientInvoker,
) error {
	if clusterCfg.Spec.Cluster.LocalRegistry != v1alpha1.LocalRegistryEnabled {
		return nil
	}

	ctx := NewLocalRegistryContext(clusterCfg, kindConfig, k3dConfig, talosConfig)

	return runLocalRegistryStage(
		cmd,
		deps,
		info,
		func(execCtx context.Context, svc registry.Service) error {
			return action(execCtx, svc, ctx)
		},
		firstActivityShown,
		dependencies,
		dockerInvoker,
	)
}

// RunLocalRegistryStage executes a local registry stage with role-based dispatch.
func RunLocalRegistryStage(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	role LocalRegistryRole,
	firstActivityShown *bool,
	dependencies LocalRegistryDependencies,
	dockerInvoker DockerClientInvoker,
) error {
	info, actionBuilder := resolveLocalRegistryStage(role)

	return RunLocalRegistryAction(
		cmd,
		clusterCfg,
		deps,
		kindConfig,
		k3dConfig,
		talosConfig,
		info,
		actionBuilder(clusterCfg),
		firstActivityShown,
		dependencies,
		dockerInvoker,
	)
}

func resolveLocalRegistryStage(
	role LocalRegistryRole,
) (LocalRegistryInfo, func(*v1alpha1.Cluster) LocalRegistryAction) {
	switch role {
	case LocalRegistryProvision:
		return ProvisionLocalRegistryInfo(), ProvisionLocalRegistry
	case LocalRegistryConnect:
		return ConnectLocalRegistryInfo(), func(_ *v1alpha1.Cluster) LocalRegistryAction {
			return ConnectLocalRegistry()
		}
	default:
		// Default to provision if role is unknown
		return ProvisionLocalRegistryInfo(), ProvisionLocalRegistry
	}
}

func newLocalRegistryCreateOptions(
	clusterCfg *v1alpha1.Cluster,
	ctx LocalRegistryContext,
) registry.CreateOptions {
	return registry.CreateOptions{
		Name:        buildLocalRegistryName(),
		Host:        registry.DefaultEndpointHost,
		Port:        resolveLocalRegistryPort(clusterCfg),
		ClusterName: ctx.ClusterName,
		VolumeName:  buildLocalRegistryName(),
	}
}

func buildLocalRegistryName() string {
	return registry.LocalRegistryContainerName
}

func resolveLocalRegistryPort(clusterCfg *v1alpha1.Cluster) int {
	if clusterCfg.Spec.Cluster.LocalRegistryOpts.HostPort > 0 {
		return int(clusterCfg.Spec.Cluster.LocalRegistryOpts.HostPort)
	}

	return dockerclient.DefaultRegistryPort
}

func runLocalRegistryStage(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	info LocalRegistryInfo,
	handler func(context.Context, registry.Service) error,
	firstActivityShown *bool,
	dependencies LocalRegistryDependencies,
	dockerInvoker DockerClientInvoker,
) error {
	return runRegistryStage(
		cmd,
		deps,
		info.ToInfo(),
		func(ctx context.Context, dockerClient client.APIClient) error {
			service, err := dependencies.ServiceFactory(registry.Config{DockerClient: dockerClient})
			if err != nil {
				return fmt.Errorf("create registry service: %w", err)
			}

			return handler(ctx, service)
		},
		firstActivityShown,
		dockerInvoker,
	)
}
