package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail-go/pkg/apis/cluster/v1alpha1"
	runtime "github.com/devantler-tech/ksail-go/pkg/di"
	configmanager "github.com/devantler-tech/ksail-go/pkg/io/config-manager"
	ksailconfigmanager "github.com/devantler-tech/ksail-go/pkg/io/config-manager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail-go/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail-go/pkg/ui/notify"
	"github.com/devantler-tech/ksail-go/pkg/ui/timer"
	"github.com/spf13/cobra"
)

// ErrMissingClusterProvisionerDependency indicates that a lifecycle command resolved a nil provisioner.
var ErrMissingClusterProvisionerDependency = errors.New("missing cluster provisioner dependency")

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
	notify.WriteMessage(
		notify.Message{
			Type:    notify.TitleType,
			Content: content,
			Emoji:   emoji,
			Writer:  cmd.OutOrStdout(),
		},
	)
}

// RunLifecycleWithConfig executes a lifecycle command using a pre-loaded cluster configuration.
// This function is useful when the cluster configuration has already been loaded, avoiding
// the need to reload it.
//
// It performs the following steps:
//  1. Create the cluster provisioner using the factory
//  2. Extract the cluster name from the distribution config
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

	clusterName, err := configmanager.GetClusterName(distributionConfig)
	if err != nil {
		return fmt.Errorf("failed to get cluster name from config: %w", err)
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
