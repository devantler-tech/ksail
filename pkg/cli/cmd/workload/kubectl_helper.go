package workload

import (
	"fmt"
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

	wrapWithKubeconfigResolution(cmd)

	return cmd
}

// wrapWithKubeconfigResolution adds a PersistentPreRunE hook that re-resolves the
// kubeconfig path after cobra has parsed all flags, honoring the --config flag.
// It chains to any existing PersistentPreRunE or PersistentPreRun on the command.
func wrapWithKubeconfigResolution(cmd *cobra.Command) {
	origPersistentPreRunE := cmd.PersistentPreRunE
	origPersistentPreRun := cmd.PersistentPreRun

	cmd.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		resolvedPath := kubeconfig.GetKubeconfigPathSilently(c)

		if f := c.Flags().Lookup("kubeconfig"); f != nil && !c.Flags().Changed("kubeconfig") {
			if err := f.Value.Set(resolvedPath); err != nil {
				return fmt.Errorf("failed to set kubeconfig flag: %w", err)
			}

			f.DefValue = resolvedPath
		}

		if origPersistentPreRunE != nil {
			return origPersistentPreRunE(c, args)
		}

		if origPersistentPreRun != nil {
			origPersistentPreRun(c, args)
		}

		return nil
	}

	cmd.PersistentPreRun = nil
}
