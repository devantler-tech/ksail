package localregistry

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/kind"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
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
		DockerInvoker:  helpers.WithDockerClient,
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
)

// StageType represents the type of local registry stage operation.
type StageType int

const (
	// StageProvision creates the local registry container.
	StageProvision StageType = iota
	// StageConnect attaches the local registry to the cluster network.
	StageConnect
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

// resolveStage returns the stage info and action builder for the given stage type.
func resolveStage(
	stage StageType,
) (setup.StageInfo, func(*v1alpha1.Cluster) stageAction, error) {
	switch stage {
	case StageProvision:
		return ProvisionStageInfo(), provisionAction, nil
	case StageConnect:
		return ConnectStageInfo(), connectActionBuilder, nil
	default:
		return setup.StageInfo{}, nil, fmt.Errorf("%w: %d", ErrUnsupportedStage, stage)
	}
}

type stageAction func(context.Context, registry.Service, registryContext) error

func provisionAction(clusterCfg *v1alpha1.Cluster) stageAction {
	return func(execCtx context.Context, svc registry.Service, ctx registryContext) error {
		createOpts := newCreateOptions(clusterCfg, ctx)

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

func connectAction() stageAction {
	return func(execCtx context.Context, svc registry.Service, ctx registryContext) error {
		startOpts := registry.StartOptions{
			Name:        registry.BuildLocalRegistryName(ctx.clusterName),
			NetworkName: ctx.networkName,
		}

		_, err := svc.Start(execCtx, startOpts)
		if err != nil {
			return fmt.Errorf("attach local registry: %w", err)
		}

		return nil
	}
}

func connectActionBuilder(_ *v1alpha1.Cluster) stageAction {
	return connectAction()
}

func runStageFromBuilder(
	cmd *cobra.Command,
	ctx *Context,
	deps lifecycle.Deps,
	info setup.StageInfo,
	buildAction func(*v1alpha1.Cluster) stageAction,
	localDeps Dependencies,
) error {
	return runRegistryAction(
		cmd,
		ctx.ClusterCfg,
		deps,
		ctx.KindConfig,
		ctx.K3dConfig,
		ctx.TalosConfig,
		info,
		buildAction(ctx.ClusterCfg),
		localDeps,
		true, false, // checkLocalRegistry=true, isCleanup=false
	)
}

// shouldSkipK3d returns true if the action should be skipped for K3d distribution.
// K3d uses native registry management via Registries.Create.
func shouldSkipK3d(clusterCfg *v1alpha1.Cluster) bool {
	return clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionK3s
}

// wrapActionWithContext wraps an action that requires registryContext into a simpler handler.
func wrapActionWithContext(
	ctx registryContext,
	action func(context.Context, registry.Service, registryContext) error,
) func(context.Context, registry.Service) error {
	return func(execCtx context.Context, svc registry.Service) error {
		return action(execCtx, svc, ctx)
	}
}

// actionRunner encapsulates shared parameters for runAction and runCleanupAction.
type actionRunner struct {
	cmd         *cobra.Command
	clusterCfg  *v1alpha1.Cluster
	deps        lifecycle.Deps
	kindConfig  *kindv1alpha4.Cluster
	k3dConfig   *k3dv1alpha5.SimpleConfig
	talosConfig *talosconfigmanager.Configs
	info        setup.StageInfo
	action      func(context.Context, registry.Service, registryContext) error
	localDeps   Dependencies
}

// prepareContext validates preconditions and creates the registry context.
// Returns nil context if the action should be skipped.
func (r *actionRunner) prepareContext(checkLocalRegistry bool) *registryContext {
	localRegistryDisabled := !r.clusterCfg.Spec.Cluster.LocalRegistry.Enabled
	if checkLocalRegistry && localRegistryDisabled {
		return nil
	}

	if shouldSkipK3d(r.clusterCfg) {
		return nil
	}

	ctx := newRegistryContext(r.clusterCfg, r.kindConfig, r.k3dConfig, r.talosConfig)

	return &ctx
}

// run executes the action with the given checkLocalRegistry flag.
// If isCleanup is true, uses runCleanupStage; otherwise uses runStage.
func (r *actionRunner) run(checkLocalRegistry, isCleanup bool) error {
	ctx := r.prepareContext(checkLocalRegistry)
	if ctx == nil {
		return nil
	}

	wrappedAction := wrapActionWithContext(*ctx, r.action)

	if isCleanup {
		return runCleanupStage(
			r.cmd, r.deps, r.info, ctx.clusterName,
			wrappedAction, r.localDeps,
		)
	}

	return runStage(
		r.cmd, r.deps, r.info, wrappedAction, r.localDeps,
	)
}

// runRegistryAction runs a registry action with the given parameters.
// checkLocalRegistry: if true, skips action when local registry is disabled in config.
// isCleanup: if true, uses cleanup stage which checks container existence instead.
func runRegistryAction(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	info setup.StageInfo,
	action func(context.Context, registry.Service, registryContext) error,
	localDeps Dependencies,
	checkLocalRegistry, isCleanup bool,
) error {
	runner := &actionRunner{
		cmd:         cmd,
		clusterCfg:  clusterCfg,
		deps:        deps,
		kindConfig:  kindConfig,
		k3dConfig:   k3dConfig,
		talosConfig: talosConfig,
		info:        info,
		action:      action,
		localDeps:   localDeps,
	}

	return runner.run(checkLocalRegistry, isCleanup)
}

// createServiceHandler creates a common handler that initializes the registry service
// and validates the context before delegating to the actual handler.
func createServiceHandler(
	localDeps Dependencies,
	handler func(context.Context, registry.Service) error,
) func(context.Context, client.APIClient) error {
	return func(ctx context.Context, dockerClient client.APIClient) error {
		service, err := localDeps.ServiceFactory(registry.Config{DockerClient: dockerClient})
		if err != nil {
			return fmt.Errorf("create registry service: %w", err)
		}

		if ctx == nil {
			return ErrNilRegistryContext
		}

		return handler(ctx, service)
	}
}

// runCleanupStage runs a cleanup stage, checking for container existence first.
// If the container doesn't exist, the stage is silently skipped (no output shown).
func runCleanupStage(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	info setup.StageInfo,
	clusterName string,
	handler func(context.Context, registry.Service) error,
	localDeps Dependencies,
) error {
	// First, check if the local registry container exists before showing any output
	var containerExists bool

	checkErr := localDeps.DockerInvoker(cmd, func(dockerClient client.APIClient) error {
		svc, err := localDeps.ServiceFactory(registry.Config{DockerClient: dockerClient})
		if err != nil {
			return fmt.Errorf("create registry service: %w", err)
		}

		registryName := registry.BuildLocalRegistryName(clusterName)

		status, statusErr := svc.Status(cmd.Context(), registry.StatusOptions{Name: registryName})
		if statusErr != nil {
			return fmt.Errorf("check registry status: %w", statusErr)
		}

		// Container exists if status is not "not provisioned"
		containerExists = status.Status != v1alpha1.OCIRegistryStatusNotProvisioned

		return nil
	})
	if checkErr != nil {
		return fmt.Errorf("check registry existence: %w", checkErr)
	}

	// If container doesn't exist, silently skip the cleanup stage
	if !containerExists {
		return nil
	}

	// Container exists, proceed with cleanup stage (which will show output)
	return runDockerStage(
		cmd,
		deps,
		info,
		createServiceHandler(localDeps, handler),
		localDeps,
	)
}

func runStage(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	info setup.StageInfo,
	handler func(context.Context, registry.Service) error,
	localDeps Dependencies,
) error {
	return runDockerStage(
		cmd,
		deps,
		info,
		createServiceHandler(localDeps, handler),
		localDeps,
	)
}

func runDockerStage(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	info setup.StageInfo,
	action func(context.Context, client.APIClient) error,
	localDeps Dependencies,
) error {
	err := setup.RunDockerStage(
		cmd,
		deps.Timer,
		info,
		action,
		localDeps.DockerInvoker,
	)
	if err != nil {
		return fmt.Errorf("run docker stage: %w", err)
	}

	return nil
}

func newRegistryContext(
	clusterCfg *v1alpha1.Cluster,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
) registryContext {
	clusterName := resolveClusterName(clusterCfg, kindConfig, k3dConfig, talosConfig)
	networkName := resolveNetworkName(clusterCfg, clusterName)

	return registryContext{clusterName: clusterName, networkName: networkName}
}

func resolveClusterName(
	clusterCfg *v1alpha1.Cluster,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
) string {
	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		return kindconfigmanager.ResolveClusterName(clusterCfg, kindConfig)
	case v1alpha1.DistributionK3s:
		return k3dconfigmanager.ResolveClusterName(clusterCfg, k3dConfig)
	case v1alpha1.DistributionTalos:
		return talosconfigmanager.ResolveClusterName(clusterCfg, talosConfig)
	default:
		if name := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Context); name != "" {
			return name
		}

		return "ksail"
	}
}

func resolveNetworkName(
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) string {
	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		return "kind"
	case v1alpha1.DistributionK3s:
		trimmed := strings.TrimSpace(clusterName)
		if trimmed == "" {
			trimmed = "k3d"
		}

		return "k3d-" + trimmed
	case v1alpha1.DistributionTalos:
		trimmed := strings.TrimSpace(clusterName)
		if trimmed == "" {
			trimmed = "talos-default"
		}

		return trimmed
	default:
		return ""
	}
}

func newCreateOptions(
	clusterCfg *v1alpha1.Cluster,
	ctx registryContext,
) registry.CreateOptions {
	return registry.CreateOptions{
		Name:        registry.BuildLocalRegistryName(ctx.clusterName),
		Host:        registry.DefaultEndpointHost,
		Port:        resolvePort(clusterCfg),
		ClusterName: ctx.clusterName,
		// Use base name for volume to share across clusters
		VolumeName: registry.LocalRegistryBaseName,
	}
}

func resolvePort(clusterCfg *v1alpha1.Cluster) int {
	if clusterCfg.Spec.Cluster.LocalRegistry.HostPort > 0 {
		return int(clusterCfg.Spec.Cluster.LocalRegistry.HostPort)
	}

	return int(v1alpha1.DefaultLocalRegistryPort)
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

	if !clusterCfg.Spec.Cluster.LocalRegistry.Enabled {
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
