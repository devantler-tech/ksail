package mirrorregistry

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/dockerutil"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// StageDefinitions maps stage roles to their definitions. Each definition's
// Actions table is keyed by distribution and references the run*Action
// implementation directly (or noopAction for cells with nothing to do, e.g.
// K3d/Talos PostClusterConnect, since those distributions configure mirrors
// before cluster creation).
//
//nolint:gochecknoglobals // Registry stage definitions are constant configuration.
var StageDefinitions = map[Role]Definition{
	RoleRegistry: {
		Info: RegistryInfo,
		Actions: map[v1alpha1.Distribution]ActionFunc{
			v1alpha1.DistributionVanilla:  runKindRegistryAction,
			v1alpha1.DistributionK3s:      runK3dRegistryAction,
			v1alpha1.DistributionTalos:    runTalosRegistryAction,
			v1alpha1.DistributionVCluster: runVClusterRegistryAction,
		},
	},
	RoleNetwork: {
		Info: NetworkInfo,
		Actions: map[v1alpha1.Distribution]ActionFunc{
			v1alpha1.DistributionVanilla:  runKindNetworkAction,
			v1alpha1.DistributionK3s:      runK3dNetworkAction,
			v1alpha1.DistributionTalos:    runTalosNetworkAction,
			v1alpha1.DistributionVCluster: runVClusterNetworkAction,
		},
	},
	RoleConnect: {
		Info: ConnectInfo,
		Actions: map[v1alpha1.Distribution]ActionFunc{
			v1alpha1.DistributionVanilla:  runKindConnectAction,
			v1alpha1.DistributionK3s:      runK3dConnectAction,
			v1alpha1.DistributionTalos:    runTalosConnectAction,
			v1alpha1.DistributionVCluster: runVClusterConnectAction,
		},
	},
	RolePostClusterConnect: {
		Info: PostClusterConnectInfo,
		Actions: map[v1alpha1.Distribution]ActionFunc{
			v1alpha1.DistributionVanilla:  runKindPostClusterConnectAction,
			v1alpha1.DistributionK3s:      noopAction,
			v1alpha1.DistributionTalos:    noopAction,
			v1alpha1.DistributionVCluster: runVClusterPostClusterConnectAction,
		},
	},
}

// DockerClientInvoker is a function that invokes Docker client operations.
// Can be overridden in tests to avoid real Docker connections.
// This is an alias to the shared setup.DockerClientInvoker type.
type DockerClientInvoker = setup.DockerClientInvoker

// DefaultDockerClientInvoker is the default Docker client invoker.
//
//nolint:gochecknoglobals // Provides default implementation with test override capability.
var DefaultDockerClientInvoker DockerClientInvoker = dockerutil.WithDockerClient

// RunStage executes the registry stage identified by role for the cluster
// described in params. It resolves the merged mirror specs, looks up the stage
// definition and the per-distribution handler, and runs the stage's prepare +
// action under the shared Docker-stage runner.
func RunStage(params StageParams, role Role) error {
	clusterCfg := params.ClusterCfg

	mirrorSpecs, err := resolveMirrorSpecs(
		params.Cmd, params.CfgManager, clusterCfg, params.TalosConfig,
	)
	if err != nil {
		return err
	}

	definition, definitionExists := StageDefinitions[role]
	if !definitionExists {
		return nil
	}

	stageCtx := &Context{
		Cmd:            params.Cmd,
		ClusterCfg:     clusterCfg,
		KindConfig:     params.KindConfig,
		K3dConfig:      params.K3dConfig,
		TalosConfig:    params.TalosConfig,
		VClusterConfig: params.VClusterConfig,
		MirrorSpecs:    mirrorSpecs,
	}

	handlers := newRegistryHandlers(stageCtx, role, definition.Actions)

	handler, ok := handlers[clusterCfg.Spec.Cluster.Distribution]
	if !ok {
		return nil
	}

	return executeRegistryStage(
		params.Cmd,
		params.Deps,
		definition.Info,
		handler.Prepare,
		handler.Action,
		params.DockerInvoker,
	)
}

// ResolveMirrorSpecs resolves the registry mirror specs for a cluster by merging
// flag-specified specs with existing specs from hosts.toml files and Talos config.
// It is used by the Kubernetes provider create path to seed the nested provisioner's
// in-DinD mirror setup (the host-level mirror stages are skipped for that provider).
func ResolveMirrorSpecs(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	talosConfig *talosconfigmanager.Configs,
) ([]registry.MirrorSpec, error) {
	return resolveMirrorSpecs(cmd, cfgManager, clusterCfg, talosConfig)
}

// resolveMirrorSpecs merges flag-specified mirror specs with existing specs
// from hosts.toml files and Talos config.
func resolveMirrorSpecs(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	talosConfig *talosconfigmanager.Configs,
) ([]registry.MirrorSpec, error) {
	mirrors := GetMirrorRegistriesWithDefaults(cmd, cfgManager, clusterCfg.Spec.Cluster.Provider)
	flagSpecs := registry.ParseMirrorSpecs(mirrors)

	existingSpecs, err := collectExistingMirrorSpecs(clusterCfg, talosConfig)
	if err != nil {
		return nil, err
	}

	return registry.MergeSpecs(existingSpecs, flagSpecs), nil
}

// collectExistingMirrorSpecs reads existing mirror specs from hosts.toml files
// and, for Talos clusters, also extracts mirror hosts from the Talos config.
func collectExistingMirrorSpecs(
	clusterCfg *v1alpha1.Cluster,
	talosConfig *talosconfigmanager.Configs,
) ([]registry.MirrorSpec, error) {
	// ReadExistingHostsToml returns (nil, nil) for missing directories.
	existingSpecs, err := registry.ReadExistingHostsToml(GetKindMirrorsDir(clusterCfg))
	if err != nil {
		return nil, fmt.Errorf("failed to read existing hosts configuration: %w", err)
	}

	if talosConfig == nil {
		return existingSpecs, nil
	}

	// Extract mirror hosts from the loaded Talos config (mirror-registries.yaml patches).
	existingHosts := make(map[string]bool, len(existingSpecs))
	for _, spec := range existingSpecs {
		existingHosts[spec.Host] = true
	}

	for _, host := range talosConfig.ExtractMirrorHosts() {
		if !existingHosts[host] {
			existingSpecs = append(existingSpecs, registry.MirrorSpec{
				Host:   host,
				Remote: registry.GenerateUpstreamURL(host),
			})
		}
	}

	return existingSpecs, nil
}

// newRegistryHandlers builds the per-distribution Handler map for a stage,
// binding each distribution's [ActionFunc] (from the stage definition's Actions
// table) to the resolved stage Context and pairing it with the distribution's
// Prepare gate. Distributions absent from actions get a Prepare that returns
// false so the stage is skipped for them.
func newRegistryHandlers(
	stageCtx *Context,
	role Role,
	actions map[v1alpha1.Distribution]ActionFunc,
) map[v1alpha1.Distribution]Handler {
	clusterCfg := stageCtx.ClusterCfg
	mirrorSpecs := stageCtx.MirrorSpecs

	bind := func(action ActionFunc) func(context.Context, dockerclient.Client) error {
		if action == nil {
			return nil
		}

		return func(execCtx context.Context, dockerClient dockerclient.Client) error {
			return action(execCtx, stageCtx, dockerClient)
		}
	}

	return map[v1alpha1.Distribution]Handler{
		v1alpha1.DistributionVanilla: {
			Prepare: func() bool {
				return PrepareKindConfigWithMirrors(clusterCfg, stageCtx.KindConfig, mirrorSpecs)
			},
			Action: bind(actions[v1alpha1.DistributionVanilla]),
		},
		v1alpha1.DistributionK3s: {
			// K3d configures registry mirrors BEFORE cluster creation via k3d config,
			// so the PostClusterConnect stage is not needed.
			Prepare: func() bool {
				if role == RolePostClusterConnect {
					return false
				}

				return PrepareK3dConfigWithMirrors(clusterCfg, stageCtx.K3dConfig, mirrorSpecs)
			},
			Action: bind(actions[v1alpha1.DistributionK3s]),
		},
		v1alpha1.DistributionTalos: {
			Prepare: func() bool {
				return PrepareTalosConfigWithMirrors(
					clusterCfg,
					stageCtx.TalosConfig,
					mirrorSpecs,
					talosconfigmanager.ResolveClusterName(clusterCfg, stageCtx.TalosConfig),
				)
			},
			Action: bind(actions[v1alpha1.DistributionTalos]),
		},
		v1alpha1.DistributionVCluster: {
			Prepare: func() bool {
				return PrepareVClusterConfigWithMirrors(clusterCfg, mirrorSpecs)
			},
			Action: bind(actions[v1alpha1.DistributionVCluster]),
		},
	}
}

func executeRegistryStage(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	info setup.StageInfo,
	shouldPrepare func() bool,
	action func(context.Context, dockerclient.Client) error,
	dockerInvoker DockerClientInvoker,
) error {
	if !shouldPrepare() {
		return nil
	}

	return runRegistryStage(cmd, deps, info, action, dockerInvoker)
}

func runRegistryStage(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	info setup.StageInfo,
	action func(context.Context, dockerclient.Client) error,
	dockerInvoker DockerClientInvoker,
) error {
	invoker := dockerInvoker
	if invoker == nil {
		invoker = DefaultDockerClientInvoker
	}

	err := setup.RunDockerStage(
		cmd,
		deps.Timer,
		info,
		action,
		invoker,
	)
	if err != nil {
		return fmt.Errorf("run registry stage: %w", err)
	}

	return nil
}

// StageParams bundles all parameters needed for registry stage execution.
// This reduces code duplication across registry stage functions.
type StageParams struct {
	Cmd            *cobra.Command
	ClusterCfg     *v1alpha1.Cluster
	Deps           lifecycle.Deps
	CfgManager     *ksailconfigmanager.ConfigManager
	KindConfig     *v1alpha4.Cluster
	K3dConfig      *v1alpha5.SimpleConfig
	TalosConfig    *talosconfigmanager.Configs
	VClusterConfig *clusterprovisioner.VClusterConfig
	DockerInvoker  DockerClientInvoker
}

// SetupRegistries creates and configures registry containers before cluster creation.
func SetupRegistries(params StageParams) error {
	return RunStage(params, RoleRegistry)
}

// CreateNetwork creates the Docker network for the cluster.
func CreateNetwork(params StageParams) error {
	return RunStage(params, RoleNetwork)
}

// ConnectRegistriesToNetwork connects registries to the Docker network before cluster creation.
func ConnectRegistriesToNetwork(params StageParams) error {
	return RunStage(params, RoleConnect)
}

// ConfigureRegistryMirrorsInCluster configures containerd inside cluster nodes after cluster creation.
func ConfigureRegistryMirrorsInCluster(params StageParams) error {
	return RunStage(params, RolePostClusterConnect)
}
