package cluster

import (
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v5/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/svc/state"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewInfoCmd creates the cluster info command.
// The command wraps kubectl cluster-info and appends TTL status when set.
func NewInfoCmd(_ *di.Runtime) *cobra.Command {
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently()
	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	})

	cmd := client.CreateClusterInfoCommand(kubeconfigPath)

	// Wrap RunE to append TTL info after kubectl cluster-info output.
	originalRunE := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := originalRunE(cmd, args); err != nil {
			return err
		}

		displayTTLInfo(kubeconfigPath)

		return nil
	}

	return cmd
}

// displayTTLInfo detects the current cluster from kubeconfig and prints TTL status if set.
func displayTTLInfo(kubeconfigPath string) {
	info, err := clusterdetector.DetectInfo(kubeconfigPath, "")
	if err != nil || info == nil {
		return
	}

	ttlInfo, err := state.LoadClusterTTL(info.ClusterName)
	if err != nil || ttlInfo == nil {
		return
	}

	// Print blank line to separate from kubectl output.
	fmt.Fprintln(os.Stdout)

	if ttlInfo.IsExpired() {
		notify.Warningf(os.Stdout,
			"cluster TTL has EXPIRED (was set to %s)", ttlInfo.Duration)
	} else {
		remaining := formatRemainingDuration(ttlInfo.Remaining())
		notify.Infof(os.Stdout,
			"cluster TTL: %s remaining (set to %s)", remaining, ttlInfo.Duration)
	}
}
