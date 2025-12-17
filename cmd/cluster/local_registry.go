package cluster

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail-go/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail-go/pkg/client/docker"
	cmdhelpers "github.com/devantler-tech/ksail-go/pkg/cmd"
	k3dconfigmanager "github.com/devantler-tech/ksail-go/pkg/io/config-manager/k3d"
	registry "github.com/devantler-tech/ksail-go/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

type localRegistryOption func(*localRegistryDependencies)

var (
	errNilRegistryContext            = errors.New("registry stage context is nil")
	errUnsupportedLocalRegistryStage = errors.New("unsupported local registry stage")
)

type localRegistryDependencies struct {
	serviceFactory func(cfg registry.Config) (registry.Service, error)
}

func defaultLocalRegistryDependencies() localRegistryDependencies {
	return localRegistryDependencies{serviceFactory: registry.NewService}
}

func newLocalRegistryDependencies(opts ...localRegistryOption) localRegistryDependencies {
	deps := defaultLocalRegistryDependencies()

	for _, opt := range opts {
		opt(&deps)
	}

	return deps
}

func localRegistryProvisionStageInfo() registryStageInfo {
	return registryStageInfo{
		title:         "Create local registry...",
		emoji:         "🗄️",
		activity:      "creating local registry",
		success:       "local registry created",
		failurePrefix: "failed to create local registry",
	}
}

func localRegistryConnectStageInfo() registryStageInfo {
	return registryStageInfo{
		title:         "Attach local registry...",
		emoji:         "🔌",
		activity:      "attaching local registry to cluster",
		success:       "local registry attached to cluster",
		failurePrefix: "failed to attach local registry",
	}
}

func localRegistryCleanupStageInfo() registryStageInfo {
	return registryStageInfo{
		title:         "Delete local registry...",
		emoji:         "🧹",
		activity:      "deleting local registry",
		success:       "local registry deleted",
		failurePrefix: "failed to delete local registry",
	}
}

type localRegistryContext struct {
	clusterName string
	networkName string
}

type localRegistryStageAction func(context.Context, registry.Service, localRegistryContext) error

type localRegistryStageExecutor func(registryStageInfo, localRegistryStageAction) error

type localRegistryStageType int

const (
	localRegistryStageProvision localRegistryStageType = iota
	localRegistryStageConnect
)

type localRegistryStageRequest struct {
	cmd        *cobra.Command
	clusterCfg *v1alpha1.Cluster
	deps       cmdhelpers.LifecycleDeps
	kindConfig *kindv1alpha4.Cluster
	k3dConfig  *k3dv1alpha5.SimpleConfig
	options    []localRegistryOption
}

func newLocalRegistryStageRequest(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	options ...localRegistryOption,
) localRegistryStageRequest {
	return localRegistryStageRequest{
		cmd:        cmd,
		clusterCfg: clusterCfg,
		deps:       deps,
		kindConfig: kindConfig,
		k3dConfig:  k3dConfig,
		options:    append([]localRegistryOption(nil), options...),
	}
}

func (r localRegistryStageRequest) run(
	info registryStageInfo,
	action func(context.Context, registry.Service, localRegistryContext) error,
	firstActivityShown *bool,
) error {
	return runLocalRegistryAction(
		r.cmd,
		r.clusterCfg,
		r.deps,
		r.kindConfig,
		r.k3dConfig,
		info,
		action,
		firstActivityShown,
		r.options...,
	)
}

func runLocalRegistryAction(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	info registryStageInfo,
	action func(context.Context, registry.Service, localRegistryContext) error,
	firstActivityShown *bool,
	options ...localRegistryOption,
) error {
	if clusterCfg.Spec.LocalRegistry != v1alpha1.LocalRegistryEnabled {
		return nil
	}

	ctx := newLocalRegistryContext(clusterCfg, kindConfig, k3dConfig)

	return runLocalRegistryStage(
		cmd,
		deps,
		info,
		func(execCtx context.Context, svc registry.Service) error {
			return action(execCtx, svc, ctx)
		},
		firstActivityShown,
		options...,
	)
}

func executeLocalRegistryStage(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	stage localRegistryStageType,
	firstActivityShown *bool,
	options ...localRegistryOption,
) error {
	info, builder, err := resolveLocalRegistryStage(stage)
	if err != nil {
		return err
	}

	return runLocalRegistryStageFromBuilder(
		cmd,
		clusterCfg,
		deps,
		kindConfig,
		k3dConfig,
		info,
		builder,
		firstActivityShown,
		options...,
	)
}

func newLocalRegistryStageExecutor(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	firstActivityShown *bool,
	options ...localRegistryOption,
) localRegistryStageExecutor {
	stage := newLocalRegistryStageRequest(
		cmd,
		clusterCfg,
		deps,
		kindConfig,
		k3dConfig,
		options...,
	)

	return func(info registryStageInfo, action localRegistryStageAction) error {
		return stage.run(info, action, firstActivityShown)
	}
}

func runLocalRegistryStageFromBuilder(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	info registryStageInfo,
	buildAction func(*v1alpha1.Cluster) localRegistryStageAction,
	firstActivityShown *bool,
	options ...localRegistryOption,
) error {
	executor := newLocalRegistryStageExecutor(
		cmd,
		clusterCfg,
		deps,
		kindConfig,
		k3dConfig,
		firstActivityShown,
		options...,
	)

	return executor(info, buildAction(clusterCfg))
}

func resolveLocalRegistryStage(
	stage localRegistryStageType,
) (registryStageInfo, func(*v1alpha1.Cluster) localRegistryStageAction, error) {
	switch stage {
	case localRegistryStageProvision:
		return localRegistryProvisionStageInfo(), provisionLocalRegistryAction, nil
	case localRegistryStageConnect:
		return localRegistryConnectStageInfo(), connectLocalRegistryActionBuilder, nil
	default:
		return registryStageInfo{}, nil, fmt.Errorf(
			"%w: %d",
			errUnsupportedLocalRegistryStage,
			stage,
		)
	}
}

func provisionLocalRegistryAction(clusterCfg *v1alpha1.Cluster) localRegistryStageAction {
	return func(execCtx context.Context, svc registry.Service, ctx localRegistryContext) error {
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

func connectLocalRegistryAction() localRegistryStageAction {
	return func(execCtx context.Context, svc registry.Service, ctx localRegistryContext) error {
		startOpts := registry.StartOptions{
			Name:        buildLocalRegistryName(),
			NetworkName: ctx.networkName,
		}

		_, err := svc.Start(execCtx, startOpts)
		if err != nil {
			return fmt.Errorf("attach local registry: %w", err)
		}

		return nil
	}
}

func connectLocalRegistryActionBuilder(_ *v1alpha1.Cluster) localRegistryStageAction {
	return connectLocalRegistryAction()
}

func cleanupLocalRegistry(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	deleteVolumes bool,
	options ...localRegistryOption,
) error {
	return cleanupLocalRegistryWithOptions(cmd, clusterCfg, deps, deleteVolumes, options...)
}

func cleanupLocalRegistryWithOptions(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	deleteVolumes bool,
	options ...localRegistryOption,
) error {
	if clusterCfg.Spec.LocalRegistry != v1alpha1.LocalRegistryEnabled {
		return nil
	}

	kindConfig, k3dConfig, err := loadDistributionConfigs(clusterCfg, deps.Timer)
	if err != nil {
		return fmt.Errorf("failed to load distribution config: %w", err)
	}

	// Cleanup doesn't show title activity messages, so use a dummy tracker
	dummyTracker := true

	return runLocalRegistryAction(
		cmd,
		clusterCfg,
		deps,
		kindConfig,
		k3dConfig,
		localRegistryCleanupStageInfo(),
		func(execCtx context.Context, svc registry.Service, ctx localRegistryContext) error {
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
				ClusterName:  ctx.clusterName,
				NetworkName:  ctx.networkName,
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
		options...,
	)
}

func newLocalRegistryContext(
	clusterCfg *v1alpha1.Cluster,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
) localRegistryContext {
	clusterName := resolveLocalRegistryClusterName(clusterCfg, kindConfig, k3dConfig)
	networkName := resolveLocalRegistryNetworkName(clusterCfg, clusterName)

	return localRegistryContext{clusterName: clusterName, networkName: networkName}
}

func resolveLocalRegistryClusterName(
	clusterCfg *v1alpha1.Cluster,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
) string {
	switch clusterCfg.Spec.Distribution {
	case v1alpha1.DistributionKind:
		if kindConfig != nil {
			if name := strings.TrimSpace(kindConfig.Name); name != "" {
				return name
			}
		}
	case v1alpha1.DistributionK3d:
		return k3dconfigmanager.ResolveClusterName(clusterCfg, k3dConfig)
	}

	if name := strings.TrimSpace(clusterCfg.Spec.Connection.Context); name != "" {
		return name
	}

	return "ksail"
}

func resolveLocalRegistryNetworkName(
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) string {
	switch clusterCfg.Spec.Distribution {
	case v1alpha1.DistributionKind:
		return "kind"
	case v1alpha1.DistributionK3d:
		trimmed := strings.TrimSpace(clusterName)
		if trimmed == "" {
			trimmed = "k3d"
		}

		return "k3d-" + trimmed
	default:
		return ""
	}
}

func newLocalRegistryCreateOptions(
	clusterCfg *v1alpha1.Cluster,
	ctx localRegistryContext,
) registry.CreateOptions {
	return registry.CreateOptions{
		Name:        buildLocalRegistryName(),
		Host:        registry.DefaultEndpointHost,
		Port:        resolveLocalRegistryPort(clusterCfg),
		ClusterName: ctx.clusterName,
		VolumeName:  buildLocalRegistryName(),
	}
}

func buildLocalRegistryName() string {
	return registry.LocalRegistryContainerName
}

func resolveLocalRegistryPort(clusterCfg *v1alpha1.Cluster) int {
	if clusterCfg.Spec.Options.LocalRegistry.HostPort > 0 {
		return int(clusterCfg.Spec.Options.LocalRegistry.HostPort)
	}

	return dockerclient.DefaultRegistryPort
}

func runLocalRegistryStage(
	cmd *cobra.Command,
	deps cmdhelpers.LifecycleDeps,
	info registryStageInfo,
	handler func(context.Context, registry.Service) error,
	firstActivityShown *bool,
	options ...localRegistryOption,
) error {
	depsConfig := newLocalRegistryDependencies(options...)

	return runRegistryStage(
		cmd,
		deps,
		info,
		func(ctx context.Context, dockerClient client.APIClient) error {
			service, err := depsConfig.serviceFactory(registry.Config{DockerClient: dockerClient})
			if err != nil {
				return fmt.Errorf("create registry service: %w", err)
			}

			if ctx == nil {
				return errNilRegistryContext
			}

			return handler(ctx, service)
		},
		firstActivityShown,
	)
}
