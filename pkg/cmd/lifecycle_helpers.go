package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/ui/notify"
	"github.com/devantler-tech/ksail/v5/pkg/ui/timer"
	"github.com/spf13/cobra"
)

// ErrMissingClusterProvisionerDependency indicates that a lifecycle command resolved a nil provisioner.
var ErrMissingClusterProvisionerDependency = errors.New("missing cluster provisioner dependency")

// ErrClusterConfigRequired indicates that a nil cluster configuration was provided.
var ErrClusterConfigRequired = errors.New("cluster configuration is required")

// LifecycleAction represents a lifecycle operation executed against a cluster provisioner.
// The action receives a context for cancellation, the provisioner instance, and the cluster name.
// It returns an error if the lifecycle operation fails.
type LifecycleAction func(
	ctx context.Context,
	provisioner clusterprovisioner.ClusterProvisioner,
	clusterName string,
) error

// LifecycleConfig describes the messaging and action behavior for a lifecycle command.
// It configures the user-facing messages displayed during command execution and specifies
// the action to perform on the cluster provisioner.
type LifecycleConfig struct {
	TitleEmoji         string
	TitleContent       string
	ActivityContent    string
	SuccessContent     string
	ErrorMessagePrefix string
	Action             LifecycleAction
}

// LifecycleDeps groups the injectable collaborators required by lifecycle commands.
type LifecycleDeps struct {
	Timer   timer.Timer
	Factory clusterprovisioner.Factory
}

// NewStandardLifecycleRunE creates a standard RunE handler for simple lifecycle commands.
// It handles dependency injection from the runtime container and delegates to HandleLifecycleRunE
// with the provided lifecycle configuration.
//
// This is the recommended way to create lifecycle command handlers for standard operations like
// start, stop, and delete. The returned function can be assigned directly to a cobra.Command's RunE field.
func NewStandardLifecycleRunE(
	runtimeContainer *runtime.Runtime,
	cfgManager *ksailconfigmanager.ConfigManager,
	config LifecycleConfig,
) func(*cobra.Command, []string) error {
	return WrapLifecycleHandler(
		runtimeContainer,
		cfgManager,
		func(cmd *cobra.Command, manager *ksailconfigmanager.ConfigManager, deps LifecycleDeps) error {
			return HandleLifecycleRunE(cmd, manager, deps, config)
		},
	)
}

// WrapLifecycleHandler resolves lifecycle dependencies from the runtime container
// and invokes the provided handler function with those dependencies.
//
// This function is used internally by NewStandardLifecycleRunE but can also be used
// directly for custom lifecycle handlers that need dependency injection but require
// custom logic beyond the standard HandleLifecycleRunE flow.
func WrapLifecycleHandler(
	runtimeContainer *runtime.Runtime,
	cfgManager *ksailconfigmanager.ConfigManager,
	handler func(*cobra.Command, *ksailconfigmanager.ConfigManager, LifecycleDeps) error,
) func(*cobra.Command, []string) error {
	return runtime.RunEWithRuntime(
		runtimeContainer,
		runtime.WithTimer(
			func(cmd *cobra.Command, injector runtime.Injector, tmr timer.Timer) error {
				factory, err := runtime.ResolveClusterProvisionerFactory(injector)
				if err != nil {
					return fmt.Errorf("resolve provisioner factory dependency: %w", err)
				}

				deps := LifecycleDeps{Timer: tmr, Factory: factory}

				return handler(cmd, cfgManager, deps)
			},
		),
	)
}

// HandleLifecycleRunE orchestrates the standard lifecycle workflow.
// It performs the following steps in order:
//  1. Start the timer
//  2. Load the cluster configuration
//  3. Create a new timer stage
//  4. Execute the lifecycle action via RunLifecycleWithConfig
//
// This function provides the complete workflow for standard lifecycle commands.
func HandleLifecycleRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps LifecycleDeps,
	config LifecycleConfig,
) error {
	if deps.Timer != nil {
		deps.Timer.Start()
	}

	outputTimer := MaybeTimer(cmd, deps.Timer)

	clusterCfg, err := cfgManager.LoadConfig(outputTimer)
	if err != nil {
		return fmt.Errorf("failed to load cluster configuration: %w", err)
	}

	if deps.Timer != nil {
		deps.Timer.NewStage()
	}

	return RunLifecycleWithConfig(cmd, deps, config, clusterCfg)
}

// showLifecycleTitle displays the title message for a lifecycle operation.
func showLifecycleTitle(cmd *cobra.Command, emoji, content string) {
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
	case v1alpha1.DistributionTalosInDocker:
		// TalosInDocker uses admin@<cluster-name> context pattern
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

// RunLifecycleWithConfig executes a lifecycle command using a pre-loaded cluster configuration.
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
func RunLifecycleWithConfig(
	cmd *cobra.Command,
	deps LifecycleDeps,
	config LifecycleConfig,
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

	return runLifecycleWithProvisioner(cmd, deps, config, provisioner, clusterName)
}

// runLifecycleWithProvisioner executes a lifecycle action using a resolved provisioner instance.
// This is an internal helper that handles the user-facing messaging and action execution.
//
// It performs the following steps:
//  1. Display the lifecycle title
//  2. Display the activity message
//  3. Execute the lifecycle action
//  4. Display success message with timing information
//
// Returns an error if the action fails.
func runLifecycleWithProvisioner(
	cmd *cobra.Command,
	deps LifecycleDeps,
	config LifecycleConfig,
	provisioner clusterprovisioner.ClusterProvisioner,
	clusterName string,
) error {
	showLifecycleTitle(cmd, config.TitleEmoji, config.TitleContent)
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

	outputTimer := MaybeTimer(cmd, deps.Timer)

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
