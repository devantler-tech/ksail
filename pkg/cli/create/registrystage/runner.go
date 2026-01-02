package registrystage

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/docker/docker/client"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// StageDefinitions maps stage roles to their definitions.
//
//nolint:gochecknoglobals // Registry stage definitions are constant configuration.
var StageDefinitions = map[Role]Definition{
	RoleRegistry: {
		Info:        RegistryInfo,
		KindAction:  KindRegistryAction,
		K3dAction:   K3dRegistryAction,
		TalosAction: TalosRegistryAction,
	},
	RoleNetwork: {
		Info:        NetworkInfo,
		KindAction:  KindNetworkAction,
		K3dAction:   K3dNetworkAction,
		TalosAction: TalosNetworkAction,
	},
	RoleConnect: {
		Info:        ConnectInfo,
		KindAction:  KindConnectAction,
		K3dAction:   K3dConnectAction,
		TalosAction: TalosConnectAction,
	},
	RolePostClusterConnect: {
		Info:        PostClusterConnectInfo,
		KindAction:  KindPostClusterConnectAction,
		K3dAction:   K3dPostClusterConnectAction,
		TalosAction: TalosPostClusterConnectAction,
	},
}

// DockerClientInvoker is a function that invokes Docker client operations.
// Can be overridden in tests to avoid real Docker connections.
type DockerClientInvoker func(*cobra.Command, func(client.APIClient) error) error

// DefaultDockerClientInvoker is the default Docker client invoker.
//
//nolint:gochecknoglobals // Provides default implementation with test override capability.
var DefaultDockerClientInvoker DockerClientInvoker = helpers.WithDockerClient

// RunStage executes the registry stage for the given role.
//
//nolint:funlen // Orchestrates multi-step registry operations with proper error handling.
func RunStage(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	cfgManager *ksailconfigmanager.ConfigManager,
	kindConfig *v1alpha4.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	role Role,
	firstActivityShown *bool,
	dockerInvoker DockerClientInvoker,
) error {
	// Get mirror specs from --mirror-registry flag
	flagSpecs := registry.ParseMirrorSpecs(
		cfgManager.Viper.GetStringSlice("mirror-registry"),
	)

	// Try to read existing hosts.toml files from the configured mirrors directory.
	// ReadExistingHostsToml returns (nil, nil) for missing directories, and an error for actual I/O issues.
	existingSpecs, err := registry.ReadExistingHostsToml(GetKindMirrorsDir(clusterCfg))
	if err != nil {
		return fmt.Errorf("failed to read existing hosts configuration: %w", err)
	}

	// For Talos, also extract mirror hosts from the loaded Talos config.
	// The Talos config includes any mirror-registries.yaml patches that were applied.
	if talosConfig != nil {
		talosHosts := talosConfig.ExtractMirrorHosts()
		for _, host := range talosHosts {
			// Only add if not already present in existingSpecs
			found := false

			for _, spec := range existingSpecs {
				if spec.Host == host {
					found = true

					break
				}
			}

			if !found {
				existingSpecs = append(existingSpecs, registry.MirrorSpec{
					Host:   host,
					Remote: registry.GenerateUpstreamURL(host),
				})
			}
		}
	}

	// Merge specs: flag specs override existing specs for the same host
	mirrorSpecs := registry.MergeSpecs(existingSpecs, flagSpecs)

	definition, definitionExists := StageDefinitions[role]
	if !definitionExists {
		return nil
	}

	stageCtx := &Context{
		Cmd:         cmd,
		ClusterCfg:  clusterCfg,
		KindConfig:  kindConfig,
		K3dConfig:   k3dConfig,
		TalosConfig: talosConfig,
		MirrorSpecs: mirrorSpecs,
	}

	handlers := newRegistryHandlers(
		clusterCfg,
		cfgManager,
		kindConfig,
		k3dConfig,
		talosConfig,
		mirrorSpecs,
		definition.KindAction(stageCtx),
		definition.K3dAction(stageCtx),
		definition.TalosAction(stageCtx),
	)

	handler, ok := handlers[clusterCfg.Spec.Cluster.Distribution]
	if !ok {
		return nil
	}

	return executeRegistryStage(
		cmd,
		deps,
		definition.Info,
		handler.Prepare,
		handler.Action,
		firstActivityShown,
		dockerInvoker,
	)
}

func newRegistryHandlers(
	clusterCfg *v1alpha1.Cluster,
	cfgManager *ksailconfigmanager.ConfigManager,
	kindConfig *v1alpha4.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	mirrorSpecs []registry.MirrorSpec,
	kindAction func(context.Context, client.APIClient) error,
	k3dAction func(context.Context, client.APIClient) error,
	talosAction func(context.Context, client.APIClient) error,
) map[v1alpha1.Distribution]Handler {
	return map[v1alpha1.Distribution]Handler{
		v1alpha1.DistributionKind: {
			Prepare: func() bool { return PrepareKindConfigWithMirrors(clusterCfg, cfgManager, kindConfig) },
			Action:  kindAction,
		},
		v1alpha1.DistributionK3d: {
			Prepare: func() bool { return PrepareK3dConfigWithMirrors(clusterCfg, k3dConfig, mirrorSpecs) },
			Action:  k3dAction,
		},
		v1alpha1.DistributionTalos: {
			Prepare: func() bool {
				return PrepareTalosConfigWithMirrors(
					clusterCfg,
					talosConfig,
					mirrorSpecs,
					ResolveTalosClusterName(talosConfig),
				)
			},
			Action: talosAction,
		},
	}
}

func executeRegistryStage(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	info Info,
	shouldPrepare func() bool,
	action func(context.Context, client.APIClient) error,
	firstActivityShown *bool,
	dockerInvoker DockerClientInvoker,
) error {
	if !shouldPrepare() {
		return nil
	}

	return runRegistryStage(cmd, deps, info, action, firstActivityShown, dockerInvoker)
}

func runRegistryStage(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	info Info,
	action func(context.Context, client.APIClient) error,
	firstActivityShown *bool,
	dockerInvoker DockerClientInvoker,
) error {
	deps.Timer.NewStage()

	if *firstActivityShown {
		cmd.Println()
	}

	*firstActivityShown = true

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: info.Title,
		Emoji:   info.Emoji,
		Writer:  cmd.OutOrStdout(),
	})

	if info.Activity != "" {
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: info.Activity,
			Writer:  cmd.OutOrStdout(),
		})
	}

	invoker := dockerInvoker
	if invoker == nil {
		invoker = DefaultDockerClientInvoker
	}

	err := invoker(cmd, func(dockerClient client.APIClient) error {
		err := action(cmd.Context(), dockerClient)
		if err != nil {
			return fmt.Errorf("%s: %w", info.FailurePrefix, err)
		}

		outputTimer := helpers.MaybeTimer(cmd, deps.Timer)

		notify.WriteMessage(notify.Message{
			Type:    notify.SuccessType,
			Content: info.Success,
			Timer:   outputTimer,
			Writer:  cmd.OutOrStdout(),
		})

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to execute registry stage: %w", err)
	}

	return nil
}

// StageParams bundles all parameters needed for registry stage execution.
// This reduces code duplication across registry stage functions.
type StageParams struct {
	Cmd                *cobra.Command
	ClusterCfg         *v1alpha1.Cluster
	Deps               lifecycle.Deps
	CfgManager         *ksailconfigmanager.ConfigManager
	KindConfig         *v1alpha4.Cluster
	K3dConfig          *v1alpha5.SimpleConfig
	TalosConfig        *talosconfigmanager.Configs
	FirstActivityShown *bool
	DockerInvoker      DockerClientInvoker
}

// SetupRegistries creates and configures registry containers before cluster creation.
func SetupRegistries(params StageParams) error {
	return runStageWithParams(params, RoleRegistry)
}

// CreateNetwork creates the Docker network for the cluster.
func CreateNetwork(params StageParams) error {
	return runStageWithParams(params, RoleNetwork)
}

// ConnectRegistriesToNetwork connects registries to the Docker network before cluster creation.
func ConnectRegistriesToNetwork(params StageParams) error {
	return runStageWithParams(params, RoleConnect)
}

// ConfigureRegistryMirrorsInCluster configures containerd inside cluster nodes after cluster creation.
func ConfigureRegistryMirrorsInCluster(params StageParams) error {
	return runStageWithParams(params, RolePostClusterConnect)
}

// runStageWithParams is the shared implementation for registry stage execution.
func runStageWithParams(params StageParams, role Role) error {
	return RunStage(
		params.Cmd,
		params.ClusterCfg,
		params.Deps,
		params.CfgManager,
		params.KindConfig,
		params.K3dConfig,
		params.TalosConfig,
		role,
		params.FirstActivityShown,
		params.DockerInvoker,
	)
}
