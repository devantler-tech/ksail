package localregistry

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// stageAction is a function that performs a registry operation within a context.
type stageAction func(context.Context, registry.Service, registryContext) error

// resolveStage returns the stage info and action builder for the given stage type.
func resolveStage(
	stage StageType,
) (setup.StageInfo, func(*v1alpha1.Cluster) stageAction, error) {
	switch stage {
	case StageProvision:
		return ProvisionStageInfo(), provisionAction, nil
	case StageConnect:
		return ConnectStageInfo(), connectActionBuilder, nil
	case StageVerify:
		// StageVerify is handled separately in RunStage via runVerifyStage
		return setup.StageInfo{}, nil, fmt.Errorf(
			"%w: StageVerify should use runVerifyStage",
			ErrUnsupportedStage,
		)
	default:
		return setup.StageInfo{}, nil, fmt.Errorf("%w: %d", ErrUnsupportedStage, stage)
	}
}

func provisionAction(clusterCfg *v1alpha1.Cluster) stageAction {
	return func(execCtx context.Context, svc registry.Service, ctx registryContext) error {
		// Cloud providers cannot use Docker-based local registries - they require
		// an external registry that the cloud nodes can reach over the internet.
		// Note: External registries are now skipped in prepareContext, so this check
		// only catches the case where a cloud provider is used without external registry.
		if isCloudProvider(clusterCfg) && !clusterCfg.Spec.Cluster.LocalRegistry.IsExternal() {
			return ErrCloudProviderRequiresExternalRegistry
		}

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
	return func(execCtx context.Context, svc registry.Service, ctx registryContext) error {
		// Note: External registries are now skipped in prepareContext,
		// so this function only handles Docker-based local registries.
		return connectAction()(execCtx, svc, ctx)
	}
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

// isCloudProvider returns true if the cluster is using a cloud provider (not Docker).
// Cloud providers run nodes on remote servers, not local Docker containers.
func isCloudProvider(clusterCfg *v1alpha1.Cluster) bool {
	return clusterCfg.Spec.Cluster.Provider == v1alpha1.ProviderHetzner
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
	localRegistryDisabled := !r.clusterCfg.Spec.Cluster.LocalRegistry.Enabled()
	if checkLocalRegistry && localRegistryDisabled {
		return nil
	}

	if shouldSkipK3d(r.clusterCfg) {
		return nil
	}

	// External registries don't need Docker-based provisioning or connection.
	// They're already running and accessible over the internet.
	if r.clusterCfg.Spec.Cluster.LocalRegistry.IsExternal() {
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
