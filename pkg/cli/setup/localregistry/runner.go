package localregistry

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	"github.com/spf13/cobra"
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
		return ConnectStageInfo(), connectAction, nil
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

// connectAction returns the stage action that attaches an existing local
// registry to the cluster network. The cluster argument is unused (the action
// derives everything it needs from the registryContext) but is present so
// connectAction satisfies the resolveStage action-builder signature directly.
//
// Note: External registries are skipped in prepareContext, so this action only
// handles Docker-based local registries.
func connectAction(_ *v1alpha1.Cluster) stageAction {
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

func runStageFromBuilder(
	cmd *cobra.Command,
	ctx *Context,
	deps lifecycle.Deps,
	info setup.StageInfo,
	buildAction func(*v1alpha1.Cluster) stageAction,
	localDeps Dependencies,
) error {
	runner := &actionRunner{
		cmd:       cmd,
		ctx:       ctx,
		deps:      deps,
		info:      info,
		action:    buildAction(ctx.ClusterCfg),
		localDeps: localDeps,
	}

	return runner.run(true) // checkLocalRegistry
}

// shouldSkipK3d returns true if the action should be skipped for K3d distribution.
// K3d uses native registry management via Registries.Create.
func shouldSkipK3d(clusterCfg *v1alpha1.Cluster) bool {
	return clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionK3s
}

// isCloudProvider returns true if the cluster is using a cloud provider (not Docker).
// Cloud providers run nodes on remote servers, not local Docker containers.
func isCloudProvider(clusterCfg *v1alpha1.Cluster) bool {
	return clusterCfg.Spec.Cluster.Provider.IsCloud()
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

// actionRunner encapsulates the parameters for running a registry stage action.
// It holds the resolved stage [Context] directly instead of re-declaring the
// per-distribution config pointers the Context already bundles.
type actionRunner struct {
	cmd       *cobra.Command
	ctx       *Context
	deps      lifecycle.Deps
	info      setup.StageInfo
	action    func(context.Context, registry.Service, registryContext) error
	localDeps Dependencies
}

// prepareContext validates preconditions and creates the registry context.
// Returns nil context if the action should be skipped.
func (r *actionRunner) prepareContext(checkLocalRegistry bool) *registryContext {
	clusterCfg := r.ctx.ClusterCfg

	localRegistryDisabled := !clusterCfg.Spec.Cluster.LocalRegistry.Enabled()
	if checkLocalRegistry && localRegistryDisabled {
		return nil
	}

	if shouldSkipK3d(clusterCfg) {
		return nil
	}

	// External registries don't need Docker-based provisioning or connection.
	// They're already running and accessible over the internet.
	if clusterCfg.Spec.Cluster.LocalRegistry.IsExternal() {
		return nil
	}

	regCtx := newRegistryContext(
		clusterCfg, r.ctx.KindConfig, r.ctx.K3dConfig, r.ctx.TalosConfig, r.ctx.VClusterConfig,
	)

	return &regCtx
}

// run executes the action with the given checkLocalRegistry flag.
func (r *actionRunner) run(checkLocalRegistry bool) error {
	regCtx := r.prepareContext(checkLocalRegistry)
	if regCtx == nil {
		return nil
	}

	handler := wrapActionWithContext(*regCtx, r.action)

	err := setup.RunDockerStage(
		r.cmd,
		r.deps.Timer,
		r.info,
		createServiceHandler(r.localDeps, handler),
		r.localDeps.DockerInvoker,
	)
	if err != nil {
		return fmt.Errorf("run docker stage: %w", err)
	}

	return nil
}

// createServiceHandler creates a common handler that initializes the registry service
// and validates the context before delegating to the actual handler.
func createServiceHandler(
	localDeps Dependencies,
	handler func(context.Context, registry.Service) error,
) func(context.Context, dockerclient.Client) error {
	return func(ctx context.Context, dockerClient dockerclient.Client) error {
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
