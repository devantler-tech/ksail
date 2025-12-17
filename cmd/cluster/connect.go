package cluster

import (
	"fmt"

	"github.com/devantler-tech/ksail-go/pkg/client/k9s"
	cmdhelpers "github.com/devantler-tech/ksail-go/pkg/cmd"
	runtime "github.com/devantler-tech/ksail-go/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail-go/pkg/io/config-manager/ksail"
	"github.com/spf13/cobra"
)

// NewConnectCmd creates the connect command for clusters.
func NewConnectCmd(_ *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect to cluster with k9s",
		Long: `Launch k9s terminal UI to interactively manage your Kubernetes cluster.
		
All k9s flags and arguments are passed through unchanged, allowing you to use
any k9s functionality. Examples:

  ksail cluster connect
  ksail cluster connect --namespace default
  ksail cluster connect --context my-context
  ksail cluster connect --readonly`,
		SilenceUsage: true,
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return HandleConnectRunE(cmd, cfgManager, args)
	}

	return cmd
}

// HandleConnectRunE handles the connect command execution.
// Exported for testing purposes.
func HandleConnectRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	args []string,
) error {
	// Load configuration
	cfg, err := cfgManager.LoadConfigSilent()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	// Get kubeconfig path with tilde expansion
	kubeConfigPath, err := cmdhelpers.GetKubeconfigPathFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("get kubeconfig path: %w", err)
	}

	// Get context from config
	context := cfg.Spec.Connection.Context

	// Create k9s client and command
	k9sClient := k9s.NewClient()
	k9sCmd := k9sClient.CreateConnectCommand(kubeConfigPath, context)

	// Transfer the context from parent command
	k9sCmd.SetContext(cmd.Context())

	// Set the args that were passed through
	k9sCmd.SetArgs(args)

	// Execute k9s command
	err = k9sCmd.Execute()
	if err != nil {
		return fmt.Errorf("execute k9s: %w", err)
	}

	return nil
}
