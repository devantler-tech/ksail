package cluster

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
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

The kubeconfig is resolved in the following priority order:
  1. From KUBECONFIG environment variable
  2. From ksail.yaml config file (if present)
  3. Defaults to ~/.kube/config

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

		kubeconfigPath, err = resolveKubeconfigForSwitch()
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
	kubeconfigPath, err := resolveKubeconfigForSwitch()
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

	sort.Strings(names)

	return names
}

// resolveKubeconfigForSwitch resolves the kubeconfig path using the same priority
// order as other cluster commands: KUBECONFIG env > ksail.yaml > default (~/.kube/config).
// When KUBECONFIG contains multiple paths separated by the OS path list separator,
// only the first path is used.
func resolveKubeconfigForSwitch() (string, error) {
	// 1. Check KUBECONFIG environment variable (use first path if multiple are specified)
	if envPath := os.Getenv("KUBECONFIG"); envPath != "" {
		paths := strings.Split(envPath, string(os.PathListSeparator))
		if len(paths) > 0 && paths[0] != "" {
			expanded, err := fsutil.ExpandHomePath(paths[0])
			if err != nil {
				return "", fmt.Errorf("expand kubeconfig path from env: %w", err)
			}

			return expanded, nil
		}
	}

	// 2. Try ksail.yaml config file, falls back to default (~/.kube/config)
	return kubeconfig.GetKubeconfigPathSilently(), nil
}
