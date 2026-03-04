package cluster

import (
	"errors"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v5/pkg/svc/detector/cluster"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
)

// ErrContextNotFound is returned when the specified context does not exist in the kubeconfig.
var ErrContextNotFound = errors.New("context not found in kubeconfig")

// switchKubeconfigFileMode is the file mode for kubeconfig files.
const switchKubeconfigFileMode = 0o600

const switchLongDesc = `Switch the active kubeconfig context to the named cluster.

This sets current-context in the kubeconfig file so that subsequent
kubectl and KSail commands target the specified cluster.

The kubeconfig path is resolved from the ksail.yaml config file if present,
or falls back to the default (~/.kube/config).

Examples:
  # Switch to a cluster named "dev"
  ksail cluster switch dev

  # Switch to a cluster named "staging"
  ksail cluster switch staging`

// NewSwitchCmd creates the switch command for clusters.
func NewSwitchCmd(_ *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "switch <name>",
		Short: "Switch active cluster context",
		Long:  switchLongDesc,
		Args:  cobra.ExactArgs(1),
		ValidArgsFunction: func(
			_ *cobra.Command,
			_ []string,
			_ string,
		) ([]string, cobra.ShellCompDirective) {
			return listContextNames(), cobra.ShellCompDirectiveNoFileComp
		},
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return HandleSwitchRunE(cmd, args[0], SwitchDeps{})
		},
	}

	return cmd
}

// SwitchDeps captures injectable dependencies for the switch command.
type SwitchDeps struct {
	// KubeconfigPath overrides the kubeconfig path resolution.
	// If empty, the path is resolved from ksail.yaml or the default.
	KubeconfigPath string
}

// HandleSwitchRunE handles the switch command.
// Exported for testing purposes.
func HandleSwitchRunE(
	cmd *cobra.Command,
	contextName string,
	deps SwitchDeps,
) error {
	kubeconfigPath := deps.KubeconfigPath
	if kubeconfigPath == "" {
		var err error

		kubeconfigPath, err = clusterdetector.ResolveKubeconfigPath("")
		if err != nil {
			return fmt.Errorf("resolve kubeconfig path: %w", err)
		}
	}

	err := switchContext(kubeconfigPath, contextName)
	if err != nil {
		return err
	}

	notify.Successf(cmd.OutOrStdout(), "Switched to cluster '%s'", contextName)

	return nil
}

// switchContext loads the kubeconfig, validates the context exists, and sets current-context.
//
//nolint:gosec // G304: kubeconfigPath is resolved from trusted config or default
func switchContext(kubeconfigPath, contextName string) error {
	configBytes, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	config, err := clientcmd.Load(configBytes)
	if err != nil {
		return fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	if _, exists := config.Contexts[contextName]; !exists {
		return fmt.Errorf("%w: %s", ErrContextNotFound, contextName)
	}

	config.CurrentContext = contextName

	result, err := clientcmd.Write(*config)
	if err != nil {
		return fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	err = os.WriteFile(kubeconfigPath, result, switchKubeconfigFileMode)
	if err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	return nil
}

// listContextNames returns all context names from the kubeconfig for shell completion.
func listContextNames() []string {
	kubeconfigPath, err := clusterdetector.ResolveKubeconfigPath("")
	if err != nil {
		return nil
	}

	//nolint:gosec // G304: kubeconfigPath is resolved from trusted config or default
	configBytes, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return nil
	}

	config, err := clientcmd.Load(configBytes)
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(config.Contexts))
	for name := range config.Contexts {
		names = append(names, name)
	}

	return names
}
