package cluster

import (
	"fmt"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/editor"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/client/k9s"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
)

// connectLead is the command-specific lead paragraph for `cluster connect`; the
// shared cluster-targeting resolution block is appended via WithClusterTargetingHelp.
const connectLead = `Launch k9s terminal UI to interactively manage your Kubernetes cluster.

The editor is determined by (in order of precedence):
  1. --editor flag
  2. spec.editor from ksail.yaml config
  3. EDITOR or VISUAL environment variables
  4. Fallback to vim, nano, or vi

k9s flags and arguments placed after a "--" separator are passed through to
k9s unchanged, allowing you to use any k9s functionality. Examples:

  ksail cluster connect
  ksail cluster connect --name dev-cluster
  ksail cluster connect --editor "code --wait"
  ksail cluster connect -- --namespace default
  ksail cluster connect -- --context my-context
  ksail cluster connect -- --readonly`

// NewConnectCmd creates the connect command for clusters.
func NewConnectCmd() *cobra.Command {
	var (
		editorFlag string
		nameFlag   string
	)

	cmd := &cobra.Command{
		Use:          "connect",
		Short:        "Connect to cluster with k9s",
		Long:         lifecycle.WithClusterTargetingHelpWithoutProvider(connectLead),
		SilenceUsage: true,
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	// Hide flags that connect doesn't use but that are needed for config
	// defaults and validation (distribution, distributionConfig, gitopsEngine,
	// localRegistry).
	hideConfigOnlyFlags(cmd)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return handleConnectRunE(cmd, cfgManager, args, editorFlag, nameFlag)
	}

	cmd.Flags().StringVar(
		&editorFlag,
		"editor",
		"",
		"editor command to use for k9s edit actions (e.g., 'code --wait', 'vim', 'nano')",
	)
	cmd.Flags().StringVarP(
		&nameFlag,
		"name", "n", "",
		"Name of the cluster to connect to (resolved like the other cluster commands; "+
			"overrides the kubeconfig context derived from ksail.yaml)",
	)

	return cmd
}

// handleConnectRunE handles the connect command execution.
func handleConnectRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	args []string,
	editorFlag string,
	nameFlag string,
) error {
	// Load configuration
	cfg, err := cfgManager.Load(configmanager.LoadOptions{Silent: true})
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	// Set up editor environment variables before connecting
	cleanup := setupEditorEnv(editorFlag, cfg)
	defer cleanup()

	// Get kubeconfig path with tilde expansion
	kubeConfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("get kubeconfig path: %w", err)
	}

	// Get context from config
	context := cfg.Spec.Cluster.Connection.Context

	// When --name is supplied, resolve the targeted cluster (flag > config >
	// kubeconfig context) and connect k9s to its context. Empty --name keeps the
	// config-derived context unchanged (existing behavior).
	if nameFlag != "" {
		resolved, resolveErr := lifecycle.ResolveClusterInfo(
			cmd,
			nameFlag,
			cfg.Spec.Cluster.Provider,
			"",
		)
		if resolveErr != nil {
			return fmt.Errorf("resolve cluster info: %w", resolveErr)
		}

		context, resolveErr = connectContextForCluster(cfg, resolved)
		if resolveErr != nil {
			return fmt.Errorf("resolve cluster context: %w", resolveErr)
		}

		if resolved.KubeconfigPath != "" {
			kubeConfigPath = resolved.KubeconfigPath
		}
	}

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

// connectContextForCluster derives the kubeconfig context k9s should target for
// a --name-resolved cluster. When the resolved name matches the config's cluster
// name, the configured context is preserved (it may carry a non-default value).
// Otherwise the targeted cluster's ACTUAL context is resolved by scanning the
// kubeconfig (distribution-agnostic, the same resolver `cluster switch` uses), so
// a cross-distribution --name target gets the right context prefix instead of
// the config's own distribution (e.g. a K3s cluster from a Vanilla config dir
// resolves to "k3d-<name>", not "kind-<name>").
func connectContextForCluster(
	cfg *v1alpha1.Cluster,
	resolved *lifecycle.ResolvedClusterInfo,
) (string, error) {
	if cfg.Name != "" && cfg.Name == resolved.ClusterName {
		return cfg.Spec.Cluster.Connection.Context, nil
	}

	config, err := clientcmd.LoadFromFile(resolved.KubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("load kubeconfig %q: %w", resolved.KubeconfigPath, err)
	}

	return resolveContextName(config, resolved.ClusterName)
}

// setupEditorEnv sets up the editor environment variables based on flag and config.
// It returns a cleanup function that should be called to restore the original environment.
func setupEditorEnv(editorFlag string, cfg *v1alpha1.Cluster) func() {
	// Create editor resolver
	resolver := editor.NewResolver(editorFlag, cfg)

	// Resolve the editor
	editorCmd := resolver.Resolve()

	// Set environment variables for connect command
	return resolver.SetEnvVars(editorCmd, "connect")
}
