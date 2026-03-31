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

	cmd.PersistentPreRunE = func(child *cobra.Command, args []string) error {
		resolvedPath := kubeconfig.GetKubeconfigPathSilently(child)

		kubeconfigFlag := child.Flags().Lookup("kubeconfig")
		if kubeconfigFlag != nil && !child.Flags().Changed("kubeconfig") {
			err := kubeconfigFlag.Value.Set(resolvedPath)
			if err != nil {
				return fmt.Errorf("failed to set kubeconfig flag: %w", err)
			}

			kubeconfigFlag.DefValue = resolvedPath
		}

		if origPersistentPreRunE != nil {
			return origPersistentPreRunE(child, args)
		}

		if origPersistentPreRun != nil {
			origPersistentPreRun(child, args)
		}

		return nil
	}

	cmd.PersistentPreRun = nil
}
