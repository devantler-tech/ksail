package cluster

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/k9s"
	pkgcmd "github.com/devantler-tech/ksail/v5/pkg/cmd"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	"github.com/spf13/cobra"
)

// NewConnectCmd creates the connect command for clusters.
func NewConnectCmd(_ *runtime.Runtime) *cobra.Command {
	var editor string

	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect to cluster with k9s",
		Long: `Launch k9s terminal UI to interactively manage your Kubernetes cluster.

The editor is determined by (in order of precedence):
  1. --editor flag
  2. spec.editor from ksail.yaml config
  3. EDITOR or VISUAL environment variables
  4. Fallback to vim, nano, or vi

All k9s flags and arguments are passed through unchanged, allowing you to use
any k9s functionality. Examples:

  ksail cluster connect
  ksail cluster connect --editor "code --wait"
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
		return HandleConnectRunE(cmd, cfgManager, args, editor)
	}

	cmd.Flags().StringVar(
		&editor,
		"editor",
		"",
		"editor command to use for k9s edit actions (e.g., 'code --wait', 'vim', 'nano')",
	)

	return cmd
}

// HandleConnectRunE handles the connect command execution.
// Exported for testing purposes.
func HandleConnectRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	args []string,
	editorFlag string,
) error {
	// Load configuration
	cfg, err := cfgManager.LoadConfigSilent()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	// Set up editor environment variables before connecting
	cleanup := setupEditorEnv(editorFlag, cfg)
	defer cleanup()

	// Get kubeconfig path with tilde expansion
	kubeConfigPath, err := pkgcmd.GetKubeconfigPathFromConfig(cfg)
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

// setupEditorEnv sets up the editor environment variables based on flag and config.
// It returns a cleanup function that should be called to restore the original environment.
func setupEditorEnv(editorFlag string, cfg *v1alpha1.Cluster) func() {
	// Create editor resolver
	resolver := pkgcmd.NewEditorResolver(editorFlag, cfg)

	// Resolve the editor
	editor := resolver.ResolveEditor()

	// Set environment variables for connect command
	return resolver.SetEditorEnvVars(editor, "connect")
}
