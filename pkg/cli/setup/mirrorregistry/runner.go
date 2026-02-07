package mirrorregistry

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
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
// This is an alias to the shared setup.DockerClientInvoker type.
type DockerClientInvoker = setup.DockerClientInvoker

// DefaultDockerClientInvoker is the default Docker client invoker.
//
//nolint:gochecknoglobals // Provides default implementation with test override capability.
var DefaultDockerClientInvoker DockerClientInvoker = helpers.WithDockerClient

// RunStage executes the registry stage for the given role.
func RunStage(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	cfgManager *ksailconfigmanager.ConfigManager,
	kindConfig *v1alpha4.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	role Role,
	dockerInvoker DockerClientInvoker,
) error {
	// Get mirror specs with defaults applied
	mirrors := GetMirrorRegistriesWithDefaults(cmd, cfgManager, clusterCfg.Spec.Cluster.Provider)
	flagSpecs := registry.ParseMirrorSpecs(mirrors)

	// Collect existing specs from hosts.toml files and Talos config
	existingSpecs, err := collectExistingMirrorSpecs(clusterCfg, talosConfig)
	if err != nil {
		return err
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
		kindConfig,
		k3dConfig,
		talosConfig,
		mirrorSpecs,
		role,
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
		dockerInvoker,
	)
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

func newRegistryHandlers(
	clusterCfg *v1alpha1.Cluster,
	kindConfig *v1alpha4.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	mirrorSpecs []registry.MirrorSpec,
	role Role,
	kindAction func(context.Context, client.APIClient) error,
	k3dAction func(context.Context, client.APIClient) error,
	talosAction func(context.Context, client.APIClient) error,
) map[v1alpha1.Distribution]Handler {
	return map[v1alpha1.Distribution]Handler{
		v1alpha1.DistributionVanilla: {
			Prepare: func() bool { return PrepareKindConfigWithMirrors(clusterCfg, kindConfig, mirrorSpecs) },
			Action:  kindAction,
		},
		v1alpha1.DistributionK3s: {
			// K3d configures registry mirrors BEFORE cluster creation via k3d config,
			// so the PostClusterConnect stage is not needed.
			Prepare: func() bool {
				if role == RolePostClusterConnect {
					return false
				}

				return PrepareK3dConfigWithMirrors(clusterCfg, k3dConfig, mirrorSpecs)
			},
			Action: k3dAction,
		},
		v1alpha1.DistributionTalos: {
			Prepare: func() bool {
				return PrepareTalosConfigWithMirrors(
					clusterCfg,
					talosConfig,
					mirrorSpecs,
					talosconfigmanager.ResolveClusterName(clusterCfg, talosConfig),
				)
			},
			Action: talosAction,
		},
	}
}

func executeRegistryStage(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	info setup.StageInfo,
	shouldPrepare func() bool,
	action func(context.Context, client.APIClient) error,
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
	action func(context.Context, client.APIClient) error,
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
	Cmd           *cobra.Command
	ClusterCfg    *v1alpha1.Cluster
	Deps          lifecycle.Deps
	CfgManager    *ksailconfigmanager.ConfigManager
	KindConfig    *v1alpha4.Cluster
	K3dConfig     *v1alpha5.SimpleConfig
	TalosConfig   *talosconfigmanager.Configs
	DockerInvoker DockerClientInvoker
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
		params.DockerInvoker,
	)
}
