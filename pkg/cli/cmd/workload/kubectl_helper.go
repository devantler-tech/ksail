package workload

import (
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// kubectlCommandCreator is a function that creates a kubectl command given a client and kubeconfig path.
type kubectlCommandCreator func(client *kubectl.Client, kubeconfigPath string) *cobra.Command

// newKubectlCommand creates a kubectl wrapper command using the provided command creator.
// The kubeconfig path is resolved lazily via a PersistentPreRunE hook so that the
// --config persistent flag is honored after cobra has parsed all flags.
func newKubectlCommand(creator kubectlCommandCreator) *cobra.Command {
	// Use a placeholder during command construction so cobra can build the
	// command tree.  The actual kubeconfig path will be resolved in
	// PersistentPreRunE before the command runs.
	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	})

	cmd := creator(client, kubeconfig.GetKubeconfigPathSilently(nil))

	// Wrap the existing PersistentPreRunE (if any) to re-resolve kubeconfig
	// after flags have been parsed, honoring --config.
	origPersistentPreRunE := cmd.PersistentPreRunE
	origPersistentPreRun := cmd.PersistentPreRun

	cmd.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		// Re-resolve kubeconfig now that flags are parsed.
		resolvedPath := kubeconfig.GetKubeconfigPathSilently(c)

		// Update the --kubeconfig default on the kubectl command tree so
		// that kubectl subcommands pick up the correct path.
		if f := c.Flags().Lookup("kubeconfig"); f != nil && !c.Flags().Changed("kubeconfig") {
			_ = f.Value.Set(resolvedPath)
			f.DefValue = resolvedPath
		}

		// Chain to the original hooks.
		if origPersistentPreRunE != nil {
			return origPersistentPreRunE(c, args)
		}

		if origPersistentPreRun != nil {
			origPersistentPreRun(c, args)
		}

		return nil
	}

	// Clear PersistentPreRun since we handle it in PersistentPreRunE above.
	cmd.PersistentPreRun = nil

	return cmd
}
