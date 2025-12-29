package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
)

// ErrMissingClusterProvisionerDependency indicates that a lifecycle command resolved a nil provisioner.
var ErrMissingClusterProvisionerDependency = errors.New("missing cluster provisioner dependency")

// ErrClusterConfigRequired indicates that a nil cluster configuration was provided.
var ErrClusterConfigRequired = errors.New("cluster configuration is required")

// Action represents a lifecycle operation executed against a cluster provisioner.
// The action receives a context for cancellation, the provisioner instance, and the cluster name.
// It returns an error if the lifecycle operation fails.
type Action func(
	ctx context.Context,
	provisioner clusterprovisioner.ClusterProvisioner,
	clusterName string,
) error

// Config describes the messaging and action behavior for a lifecycle command.
// It configures the user-facing messages displayed during command execution and specifies
// the action to perform on the cluster provisioner.
type Config struct {
	TitleEmoji         string
	TitleContent       string
	ActivityContent    string
	SuccessContent     string
	ErrorMessagePrefix string
	Action             Action
}

// Deps groups the injectable collaborators required by lifecycle commands.
type Deps struct {
	Timer   timer.Timer
	Factory clusterprovisioner.Factory
}

// NewStandardRunE creates a standard RunE handler for simple lifecycle commands.
// It handles dependency injection from the runtime container and delegates to HandleRunE
// with the provided lifecycle configuration.
//
// This is the recommended way to create lifecycle command handlers for standard operations like
// start, stop, and delete. The returned function can be assigned directly to a cobra.Command's RunE field.
func NewStandardRunE(
	runtimeContainer *runtime.Runtime,
	cfgManager *ksailconfigmanager.ConfigManager,
	config Config,
) func(*cobra.Command, []string) error {
	return WrapHandler(
		runtimeContainer,
		cfgManager,
		func(cmd *cobra.Command, manager *ksailconfigmanager.ConfigManager, deps Deps) error {
			return HandleRunE(cmd, manager, deps, config)
		},
	)
}

// WrapHandler resolves lifecycle dependencies from the runtime container
// and invokes the provided handler function with those dependencies.
//
// This function loads the cluster configuration first, then creates a factory
// using the cached distribution config from the config manager. This ensures
// the factory has the proper distribution-specific configuration.
//
// This function is used internally by NewStandardRunE but can also be used
// directly for custom lifecycle handlers that need dependency injection but require
// custom logic beyond the standard HandleRunE flow.
func WrapHandler(
	runtimeContainer *runtime.Runtime,
	cfgManager *ksailconfigmanager.ConfigManager,
	handler func(*cobra.Command, *ksailconfigmanager.ConfigManager, Deps) error,
) func(*cobra.Command, []string) error {
	return runtime.RunEWithRuntime(
		runtimeContainer,
		runtime.WithTimer(
			func(cmd *cobra.Command, _ runtime.Injector, tmr timer.Timer) error {
				// Start timer and load config first to get distribution config
				if tmr != nil {
					tmr.Start()
				}

				outputTimer := helpers.MaybeTimer(cmd, tmr)

				_, err := cfgManager.LoadConfig(outputTimer)
				if err != nil {
					return fmt.Errorf("failed to load cluster configuration: %w", err)
				}

				// Create factory with the cached distribution config
				factory := clusterprovisioner.DefaultFactory{
					DistributionConfig: cfgManager.DistributionConfig,
				}

				deps := Deps{Timer: tmr, Factory: factory}

				return handler(cmd, cfgManager, deps)
			},
		),
	)
}

// HandleRunE orchestrates the standard lifecycle workflow.
// It performs the following steps in order:
//  1. Create a new timer stage (config was already loaded in WrapHandler)
//  2. Execute the lifecycle action via RunWithConfig
//
// Note: The cluster configuration is already loaded by WrapHandler,
// so this function uses the cached config from cfgManager.Config.
func HandleRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps Deps,
	config Config,
) error {
	// Config is already loaded by WrapHandler, so we use the cached config
	clusterCfg := cfgManager.Config

	if deps.Timer != nil {
		deps.Timer.NewStage()
	}

	return RunWithConfig(cmd, deps, config, clusterCfg)
}

// showTitle displays the title message for a lifecycle operation.
func showTitle(cmd *cobra.Command, emoji, content string) {
	_, _ = fmt.Fprintln(cmd.OutOrStdout()) // Add newline before title for visual separation
	notify.WriteMessage(
		notify.Message{
			Type:    notify.TitleType,
			Content: content,
			Emoji:   emoji,
			Writer:  cmd.OutOrStdout(),
		},
	)
}

// getClusterNameFromConfigOrContext extracts the cluster name, preferring the context if set.
// When a context is explicitly provided (e.g., "kind-my-cluster"), it derives the cluster name
// from it (e.g., "my-cluster"). Otherwise, it falls back to the distribution config name.
func getClusterNameFromConfigOrContext(
	distributionConfig any,
	clusterCfg *v1alpha1.Cluster,
) (string, error) {
	// If context is explicitly set, derive cluster name from it
	if clusterCfg != nil && clusterCfg.Spec.Cluster.Connection.Context != "" {
		clusterName := ExtractClusterNameFromContext(
			clusterCfg.Spec.Cluster.Connection.Context,
			clusterCfg.Spec.Cluster.Distribution,
		)
		if clusterName != "" {
			return clusterName, nil
		}
	}

	// Fall back to distribution config name
	clusterName, err := configmanager.GetClusterName(distributionConfig)
	if err != nil {
		return "", fmt.Errorf("failed to get cluster name from distribution config: %w", err)
	}

	return clusterName, nil
}

// ExtractClusterNameFromContext extracts the cluster name from a context string.
// For kind clusters, contexts follow the pattern "kind-<cluster-name>".
// For k3d clusters, contexts follow the pattern "k3d-<cluster-name>".
// Returns empty string if the context doesn't match the expected pattern.
func ExtractClusterNameFromContext(context string, distribution v1alpha1.Distribution) string {
	switch distribution {
	case v1alpha1.DistributionKind:
		if clusterName, ok := strings.CutPrefix(context, "kind-"); ok {
			return clusterName
		}
	case v1alpha1.DistributionK3d:
		if clusterName, ok := strings.CutPrefix(context, "k3d-"); ok {
			return clusterName
		}
	case v1alpha1.DistributionTalos:
		// Talos uses admin@<cluster-name> context pattern
		if clusterName, ok := strings.CutPrefix(context, "admin@"); ok {
			return clusterName
		}
	}

	return ""
}

// GetClusterNameFromConfig extracts the cluster name from the KSail cluster configuration.
// When a context is explicitly set, it derives the cluster name from it.
// Otherwise, it loads the distribution config and extracts the name from there.
// This function is exported for use in command handlers that need the cluster name
// for operations beyond the standard lifecycle actions.
func GetClusterNameFromConfig(
	clusterCfg *v1alpha1.Cluster,
	factory clusterprovisioner.Factory,
) (string, error) {
	if clusterCfg == nil {
		return "", ErrClusterConfigRequired
	}

	// If context is explicitly set, derive cluster name from it
	if clusterCfg.Spec.Cluster.Connection.Context != "" {
		clusterName := ExtractClusterNameFromContext(
			clusterCfg.Spec.Cluster.Connection.Context,
			clusterCfg.Spec.Cluster.Distribution,
		)
		if clusterName != "" {
			return clusterName, nil
		}
	}

	// Fall back to distribution config name
	_, distributionConfig, err := factory.Create(context.Background(), clusterCfg)
	if err != nil {
		return "", fmt.Errorf("failed to load distribution config: %w", err)
	}

	clusterName, err := configmanager.GetClusterName(distributionConfig)
	if err != nil {
		return "", fmt.Errorf("failed to get cluster name from distribution config: %w", err)
	}

	return clusterName, nil
}

// RunWithConfig executes a lifecycle command using a pre-loaded cluster configuration.
// This function is useful when the cluster configuration has already been loaded, avoiding
// the need to reload it.
//
// It performs the following steps:
//  1. Create the cluster provisioner using the factory
//  2. Extract the cluster name from the distribution config or context
//  3. Execute the lifecycle action
//  4. Display success message with timing information
//
// Returns an error if provisioner creation, cluster name extraction, or the action itself fails.
func RunWithConfig(
	cmd *cobra.Command,
	deps Deps,
	config Config,
	clusterCfg *v1alpha1.Cluster,
) error {
	provisioner, distributionConfig, err := deps.Factory.Create(cmd.Context(), clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to resolve cluster provisioner: %w", err)
	}

	if provisioner == nil {
		return ErrMissingClusterProvisionerDependency
	}

	clusterName, err := getClusterNameFromConfigOrContext(distributionConfig, clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to get cluster name: %w", err)
	}

	return runWithProvisioner(cmd, deps, config, provisioner, clusterName)
}

// runWithProvisioner executes a lifecycle action using a resolved provisioner instance.
// This is an internal helper that handles the user-facing messaging and action execution.
//
// It performs the following steps:
//  1. Display the lifecycle title
//  2. Display the activity message
//  3. Execute the lifecycle action
//  4. Display success message with timing information
//
// Returns an error if the action fails.
func runWithProvisioner(
	cmd *cobra.Command,
	deps Deps,
	config Config,
	provisioner clusterprovisioner.ClusterProvisioner,
	clusterName string,
) error {
	showTitle(cmd, config.TitleEmoji, config.TitleContent)
	notify.WriteMessage(
		notify.Message{
			Type:    notify.ActivityType,
			Content: config.ActivityContent,
			Writer:  cmd.OutOrStdout(),
		},
	)

	err := config.Action(cmd.Context(), provisioner, clusterName)
	if err != nil {
		return fmt.Errorf("%s: %w", config.ErrorMessagePrefix, err)
	}

	outputTimer := helpers.MaybeTimer(cmd, deps.Timer)

	notify.WriteMessage(
		notify.Message{
			Type:    notify.SuccessType,
			Content: config.SuccessContent,
			Timer:   outputTimer,
			Writer:  cmd.OutOrStdout(),
		},
	)

	return nil
}
