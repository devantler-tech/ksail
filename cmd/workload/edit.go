package workload

import (
	"os"

	"github.com/devantler-tech/ksail/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/pkg/client/kubectl"
	pkgcmd "github.com/devantler-tech/ksail/pkg/cmd"
	cmdhelpers "github.com/devantler-tech/ksail/pkg/cmd"
	ksailconfigmanager "github.com/devantler-tech/ksail/pkg/io/config-manager/ksail"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewEditCmd creates the workload edit command.
func NewEditCmd() *cobra.Command {
	var editor string

	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit a resource",
		Long: `Edit a Kubernetes resource from the default editor.

The editor is determined by (in order of precedence):
  1. --editor flag
  2. spec.editor from ksail.yaml config
  3. KUBE_EDITOR, EDITOR, or VISUAL environment variables
  4. Fallback to vim, nano, or vi

Example:
  ksail workload edit deployment/my-deployment
  ksail workload edit --editor "code --wait" deployment/my-deployment`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Set up editor environment variables before edit
			cleanup := setupEditorEnv(editor)
			defer cleanup()

			// Try to load config silently to get kubeconfig path
			kubeconfigPath := cmdhelpers.GetKubeconfigPathSilently()

			// Create IO streams for kubectl
			ioStreams := genericiooptions.IOStreams{
				In:     os.Stdin,
				Out:    os.Stdout,
				ErrOut: os.Stderr,
			}

			// Create kubectl client and get the edit command directly
			client := kubectl.NewClient(ioStreams)
			editCmd := client.CreateEditCommand(kubeconfigPath)

			// Transfer the context from parent command
			editCmd.SetContext(cmd.Context())

			// Set the args that were passed through
			editCmd.SetArgs(args)

			// Execute kubectl edit command
			return editCmd.Execute()
		},
	}

	cmd.Flags().StringVar(
		&editor,
		"editor",
		"",
		"editor command to use (e.g., 'code --wait', 'vim', 'nano')",
	)

	return cmd
}

// setupEditorEnv sets up the editor environment variables based on flag and config.
// It returns a cleanup function that should be called to restore the original environment.
func setupEditorEnv(editorFlag string) func() {
	// Try to load config silently (don't error if it fails)
	var cfg *v1alpha1.Cluster

	fieldSelectors := ksailconfigmanager.DefaultClusterFieldSelectors()
	cfgManager := ksailconfigmanager.NewConfigManager(nil, fieldSelectors...)

	loadedCfg, err := cfgManager.LoadConfigSilent()
	if err == nil {
		cfg = loadedCfg
	}

	// Create editor resolver
	resolver := pkgcmd.NewEditorResolver(editorFlag, cfg)

	// Resolve the editor
	editor := resolver.ResolveEditor()

	// Set environment variables for workload command
	return resolver.SetEditorEnvVars(editor, "workload")
}
