package workload

import (
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/editor"
	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewEditCmd creates the workload edit command.
func NewEditCmd() *cobra.Command {
	var editorFlag string

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
			cleanup := editor.SetupEditorEnv(editorFlag, "workload")
			defer cleanup()

			// Try to load config silently to get kubeconfig path
			kubeconfigPath := kubeconfig.GetKubeconfigPathSilently()

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
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	cmd.Flags().StringVar(
		&editorFlag,
		"editor",
		"",
		"editor command to use (e.g., 'code --wait', 'vim', 'nano')",
	)

	return cmd
}
