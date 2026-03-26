package cluster

import (
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v5/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/svc/state"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewInfoCmd creates the cluster info command.
// The command wraps kubectl cluster-info and appends TTL status when set.
func NewInfoCmd(_ *di.Runtime) *cobra.Command {
	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	})

	// kubectl requires a kubeconfig path at construction time to wire its
	// ConfigFlags defaults.  We pass the default path here and override it
	// in RunE (after cobra has parsed flags) so that --config is honored.
	cmd := client.CreateClusterInfoCommand(k8s.DefaultKubeconfigPath())

	// Wrap RunE to resolve kubeconfig at runtime (honoring --config flag)
	// and append TTL info after kubectl cluster-info output.
	originalRunE := cmd.RunE
	if originalRunE != nil {
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(cmd)
			// Override kubectl's --kubeconfig default when the user has not
			// explicitly set it via --kubeconfig themselves.
			if f := cmd.Flag("kubeconfig"); f != nil && !f.Changed {
				err := f.Value.Set(kubeconfigPath)
				if err != nil {
					return fmt.Errorf("set kubeconfig flag: %w", err)
				}
			}

			err := originalRunE(cmd, args)
			if err != nil {
				return err
			}

			displayTTLInfo(cmd, kubeconfigPath)

			return nil
		}
	}

	return cmd
}

// displayTTLInfo detects the current cluster from kubeconfig and prints TTL status if set.
func displayTTLInfo(cmd *cobra.Command, kubeconfigPath string) {
	info, err := clusterdetector.DetectInfo(kubeconfigPath, "")
	if err != nil || info == nil {
		return
	}

	ttlInfo, err := state.LoadClusterTTL(info.ClusterName)
	if err != nil || ttlInfo == nil {
		return
	}

	writer := cmd.OutOrStdout()

	// Print blank line to separate from kubectl output.
	_, _ = fmt.Fprintln(writer)

	remaining := ttlInfo.Remaining()
	if remaining <= 0 {
		notify.Warningf(writer,
			"cluster TTL has EXPIRED (was set to %s)", ttlInfo.Duration)
	} else {
		notify.Infof(
			writer,
			"cluster TTL: %s remaining (set to %s)",
			formatRemainingDuration(remaining),
			ttlInfo.Duration,
		)
	}
}
