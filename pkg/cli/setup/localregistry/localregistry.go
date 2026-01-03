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

// Dependencies holds injectable dependencies for local registry operations.
type Dependencies struct {
	ServiceFactory func(cfg registry.Config) (registry.Service, error)
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
	firstActivityShown *bool,
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
		firstActivityShown,
		localDeps,
	)
}

// Cleanup cleans up the local registry during cluster deletion.
func Cleanup(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	deleteVolumes bool,
	localDeps Dependencies,
) error {
	if clusterCfg.Spec.Cluster.LocalRegistry != v1alpha1.LocalRegistryEnabled {
		return nil
	}

	// Use cached distribution config from ConfigManager
	distConfig := cfgManager.DistributionConfig

	// Cleanup doesn't show title activity messages, so use a dummy tracker
	dummyTracker := true

	return runAction(
		cmd,
		clusterCfg,
		deps,
		distConfig.Kind,
		distConfig.K3d,
		distConfig.Talos,
		CleanupStageInfo(),
		func(execCtx context.Context, svc registry.Service, regCtx registryContext) error {
			registryName := buildRegistryName()
			volumeName := registryName

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
		&dummyTracker,
		localDeps,
	)
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
			Name:        buildRegistryName(),
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
	firstActivityShown *bool,
	localDeps Dependencies,
) error {
	return runAction(
		cmd,
		ctx.ClusterCfg,
		deps,
		ctx.KindConfig,
		ctx.K3dConfig,
		ctx.TalosConfig,
		info,
		buildAction(ctx.ClusterCfg),
		firstActivityShown,
		localDeps,
	)
}

func runAction(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	info setup.StageInfo,
	action func(context.Context, registry.Service, registryContext) error,
	firstActivityShown *bool,
	localDeps Dependencies,
) error {
	if clusterCfg.Spec.Cluster.LocalRegistry != v1alpha1.LocalRegistryEnabled {
		return nil
	}

	ctx := newRegistryContext(clusterCfg, kindConfig, k3dConfig, talosConfig)

	return runStage(
		cmd,
		deps,
		info,
		func(execCtx context.Context, svc registry.Service) error {
			return action(execCtx, svc, ctx)
		},
		firstActivityShown,
		localDeps,
	)
}

func runStage(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	info setup.StageInfo,
	handler func(context.Context, registry.Service) error,
	firstActivityShown *bool,
	localDeps Dependencies,
) error {
	return runDockerStage(
		cmd,
		deps,
		info,
		func(ctx context.Context, dockerClient client.APIClient) error {
			service, err := localDeps.ServiceFactory(registry.Config{DockerClient: dockerClient})
			if err != nil {
				return fmt.Errorf("create registry service: %w", err)
			}

			if ctx == nil {
				return ErrNilRegistryContext
			}

			return handler(ctx, service)
		},
		firstActivityShown,
		localDeps,
	)
}

func runDockerStage(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	info setup.StageInfo,
	action func(context.Context, client.APIClient) error,
	firstActivityShown *bool,
	localDeps Dependencies,
) error {
	err := setup.RunDockerStage(
		cmd,
		deps.Timer,
		info,
		action,
		firstActivityShown,
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
	case v1alpha1.DistributionKind:
		return kindconfigmanager.ResolveClusterName(clusterCfg, kindConfig)
	case v1alpha1.DistributionK3d:
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
	case v1alpha1.DistributionKind:
		return "kind"
	case v1alpha1.DistributionK3d:
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
		Name:        buildRegistryName(),
		Host:        registry.DefaultEndpointHost,
		Port:        resolvePort(clusterCfg),
		ClusterName: ctx.clusterName,
		VolumeName:  buildRegistryName(),
	}
}

func buildRegistryName() string {
	return registry.LocalRegistryContainerName
}

func resolvePort(clusterCfg *v1alpha1.Cluster) int {
	if clusterCfg.Spec.Cluster.LocalRegistryOpts.HostPort > 0 {
		return int(clusterCfg.Spec.Cluster.LocalRegistryOpts.HostPort)
	}

	return dockerclient.DefaultRegistryPort
}
