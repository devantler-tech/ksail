package localregistry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers/docker"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/client/oci"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/configmanager/k3d"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/configmanager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/configmanager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// Registry verification constants.
const (
	registryVerifyTimeout = 10 * time.Second
)

// Option configures local registry dependencies.
type Option func(*Dependencies)

// ServiceFactoryFunc is a function type for creating registry services.
type ServiceFactoryFunc func(cfg registry.Config) (registry.Service, error)

// Dependencies holds injectable dependencies for local registry operations.
type Dependencies struct {
	ServiceFactory ServiceFactoryFunc
	DockerInvoker  func(*cobra.Command, func(client.APIClient) error) error
}

// DefaultDependencies returns the default production dependencies.
func DefaultDependencies() Dependencies {
	return Dependencies{
		ServiceFactory: registry.NewService,
		DockerInvoker:  docker.WithDockerClient,
	}
}

// NewDependencies creates dependencies with optional overrides.
func NewDependencies(opts ...Option) Dependencies {
	deps := DefaultDependencies()

	for _, opt := range opts {
		opt(&deps)
	}

	return deps
}

// WithServiceFactory sets a custom service factory.
func WithServiceFactory(factory func(cfg registry.Config) (registry.Service, error)) Option {
	return func(d *Dependencies) {
		d.ServiceFactory = factory
	}
}

// WithDockerInvoker sets a custom Docker client invoker.
func WithDockerInvoker(invoker func(*cobra.Command, func(client.APIClient) error) error) Option {
	return func(d *Dependencies) {
		d.DockerInvoker = invoker
	}
}

// Errors for local registry operations.
var (
	ErrNilRegistryContext = errors.New("registry stage context is nil")
	ErrUnsupportedStage   = errors.New("unsupported local registry stage")
	// ErrCloudProviderRequiresExternalRegistry is returned when a cloud provider
	// is used with a Docker-based local registry instead of an external one.
	ErrCloudProviderRequiresExternalRegistry = errors.New(
		"cloud provider requires an external registry\n" +
			"- use --local-registry with an internet-accessible registry (e.g., ghcr.io/myorg)",
	)
)

// StageType represents the type of local registry stage operation.
type StageType int

const (
	// StageProvision creates the local registry container.
	StageProvision StageType = iota
	// StageConnect attaches the local registry to the cluster network.
	StageConnect
	// StageVerify checks write access to the registry.
	StageVerify
)

// Context holds cluster configuration for local registry operations.
type Context struct {
	ClusterCfg  *v1alpha1.Cluster
	KindConfig  *kindv1alpha4.Cluster
	K3dConfig   *k3dv1alpha5.SimpleConfig
	TalosConfig *talosconfigmanager.Configs
}

// NewContextFromConfigManager creates a Context from a config manager.
func NewContextFromConfigManager(cfgManager *ksailconfigmanager.ConfigManager) *Context {
	distConfig := cfgManager.DistributionConfig

	return &Context{
		ClusterCfg:  cfgManager.Config,
		KindConfig:  distConfig.Kind,
		K3dConfig:   distConfig.K3d,
		TalosConfig: distConfig.Talos,
	}
}

// registryContext holds derived values for registry operations.
type registryContext struct {
	clusterName string
	networkName string
}

// ProvisionStageInfo returns the stage info for provisioning.
func ProvisionStageInfo() setup.StageInfo {
	return setup.StageInfo{
		Title:         "Create local registry...",
		Emoji:         "🗄️",
		Activity:      "creating local registry",
		Success:       "local registry created",
		FailurePrefix: "failed to create local registry",
	}
}

// ConnectStageInfo returns the stage info for connecting.
func ConnectStageInfo() setup.StageInfo {
	return setup.StageInfo{
		Title:         "Attach local registry...",
		Emoji:         "🔌",
		Activity:      "attaching local registry to cluster",
		Success:       "local registry attached to cluster",
		FailurePrefix: "failed to attach local registry",
	}
}

// VerifyStageInfo returns the stage info for verifying registry access.
func VerifyStageInfo() setup.StageInfo {
	return setup.StageInfo{
		Title:         "Verify registry access...",
		Emoji:         "🔐",
		Activity:      "verifying registry write access",
		Success:       "registry access verified",
		FailurePrefix: "registry access check failed",
	}
}

// CleanupStageInfo returns the stage info for cleanup.
func CleanupStageInfo() setup.StageInfo {
	return setup.StageInfo{
		Title:         "Delete local registry...",
		Emoji:         "🧹",
		Activity:      "deleting local registry",
		Success:       "local registry deleted",
		FailurePrefix: "failed to delete local registry",
	}
}

// ExecuteStage executes the specified local registry stage.
func ExecuteStage(
	cmd *cobra.Command,
	ctx *Context,
	deps lifecycle.Deps,
	stage StageType,
	localDeps Dependencies,
) error {
	info, actionBuilder, err := resolveStage(stage)
	if err != nil {
		return err
	}

	return runStageFromBuilder(
		cmd,
		ctx,
		deps,
		info,
		actionBuilder,
		localDeps,
	)
}

// VerifyRegistryAccess checks if we have write access to the configured registry.
// For external registries, this verifies authentication and permissions.
// For local Docker registries, this is skipped (no auth required).
// This should be called after the provision stage to give early feedback about auth issues.
func VerifyRegistryAccess(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
) error {
	localRegistry := clusterCfg.Spec.Cluster.LocalRegistry
	if !localRegistry.Enabled() || !localRegistry.IsExternal() {
		return nil
	}

	// Show verification stage
	info := VerifyStageInfo()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: info.Title,
		Emoji:   info.Emoji,
		Writer:  cmd.OutOrStdout(),
	})

	deps.Timer.NewStage()

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: info.Activity,
		Writer:  cmd.OutOrStdout(),
	})

	verifyOpts := buildVerifyOptions(clusterCfg)

	err := oci.VerifyRegistryAccessWithTimeout(cmd.Context(), verifyOpts, registryVerifyTimeout)
	if err != nil {
		//nolint:wrapcheck // Error from VerifyRegistryAccessWithTimeout is already well-formatted
		return err
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: info.Success,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// Cleanup cleans up the local registry during cluster deletion.
// This function checks if the local registry container exists and removes it if present,
// regardless of the config setting. This ensures orphaned containers are cleaned up
// even when the config file is missing or has default values.
func Cleanup(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	deleteVolumes bool,
	localDeps Dependencies,
) error {
	// K3d uses native registry management via Registries.Create.
	// K3d automatically creates, connects, and manages the registry container
	// as part of cluster creation, so we skip KSail's manual registry handling.
	if clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionK3s {
		return nil
	}

	// Use cached distribution config from ConfigManager
	distConfig := cfgManager.DistributionConfig

	return runRegistryAction(
		cmd,
		clusterCfg,
		deps,
		distConfig.Kind,
		distConfig.K3d,
		distConfig.Talos,
		CleanupStageInfo(),
		func(execCtx context.Context, svc registry.Service, regCtx registryContext) error {
			registryName := registry.BuildLocalRegistryName(regCtx.clusterName)
			// Use base name for volume to share across clusters
			volumeName := registry.LocalRegistryBaseName

			if deleteVolumes {
				status, statusErr := svc.Status(execCtx, registry.StatusOptions{Name: registryName})
				if statusErr == nil && strings.TrimSpace(status.VolumeName) != "" {
					volumeName = status.VolumeName
				}
			}

			stopOpts := registry.StopOptions{
				Name:         registryName,
				ClusterName:  regCtx.clusterName,
				NetworkName:  regCtx.networkName,
				DeleteVolume: deleteVolumes,
				VolumeName:   volumeName,
			}

			err := svc.Stop(execCtx, stopOpts)
			if err != nil {
				return fmt.Errorf("stop local registry: %w", err)
			}

			return nil
		},
		localDeps,
		false, true, // checkLocalRegistry=false, isCleanup=true
	)
}

// Disconnect disconnects the local registry from the cluster network without deleting it.
// This is used for Talos to allow the cluster network to be removed during deletion
// without "active endpoints" errors. The container cleanup happens after cluster deletion.
func Disconnect(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	_ lifecycle.Deps,
	clusterName string,
	localDeps Dependencies,
) error {
	// K3d uses native registry management, skip for K3d
	if clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionK3s {
		return nil
	}

	// Only disconnect for Talos - Kind uses a shared "kind" network
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionTalos {
		return nil
	}

	distConfig := cfgManager.DistributionConfig

	regCtx := newRegistryContext(clusterCfg, distConfig.Kind, distConfig.K3d, distConfig.Talos)
	registryName := registry.BuildLocalRegistryName(regCtx.clusterName)

	// Use the cluster name as the network name for Talos
	networkName := clusterName

	return localDeps.DockerInvoker(cmd, func(dockerClient client.APIClient) error {
		registryMgr, err := dockerclient.NewRegistryManager(dockerClient)
		if err != nil {
			return fmt.Errorf("create registry manager: %w", err)
		}

		return registryMgr.DisconnectFromNetwork(cmd.Context(), registryName, networkName)
	})
}

// WaitForK3dLocalRegistryReady waits for the K3d-managed local registry to be ready.
// This should be called after K3d cluster creation when local registry is enabled,
// to ensure the registry is accepting connections before installing Flux or other
// components that depend on it.
//
// For K3d, the local registry is created during cluster creation via Registries.Create,
// so we need to wait for it to be ready after the cluster is created.
// For Kind and Talos, this is a no-op since they use KSail-managed registries
// which are created and waited for before cluster creation.
func WaitForK3dLocalRegistryReady(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	dockerInvoker func(*cobra.Command, func(client.APIClient) error) error,
) error {
	// Only wait for K3d with local registry enabled
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionK3s {
		return nil
	}

	if !clusterCfg.Spec.Cluster.LocalRegistry.Enabled() {
		return nil
	}

	clusterName := k3dconfigmanager.ResolveClusterName(clusterCfg, k3dConfig)
	registryName := registry.BuildLocalRegistryName(clusterName)

	return dockerInvoker(cmd, func(dockerClient client.APIClient) error {
		registryMgr, err := dockerclient.NewRegistryManager(dockerClient)
		if err != nil {
			return fmt.Errorf("failed to create registry manager: %w", err)
		}

		// Wait for the K3d-managed local registry to be ready.
		// Use WaitForContainerRegistryReady which doesn't require KSail labels,
		// since K3d creates the registry container without KSail labels.
		err = registryMgr.WaitForContainerRegistryReady(
			cmd.Context(),
			registryName,
			dockerclient.RegistryReadyTimeout,
		)
		if err != nil {
			return fmt.Errorf("local registry not ready: %w", err)
		}

		return nil
	})
}
