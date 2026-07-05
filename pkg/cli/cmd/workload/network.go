package workload

import (
	"errors"
	"fmt"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/hubble"
	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/spf13/cobra"
)

// defaultFlowCount is the number of most-recent flows fetched when --last is
// not set.
const defaultFlowCount = 20

// ErrCNINotCilium is returned when the configured cluster does not use the
// Cilium CNI, since Hubble flow observability requires Cilium.
var ErrCNINotCilium = errors.New(
	"network observability requires the Cilium CNI; " +
		"set 'spec.cluster.cni: Cilium' in ksail.yaml (or '--cni Cilium' at project init)",
)

// newFlowObserver builds the Hubble flow observer the network command uses.
//
//nolint:gochecknoglobals // Test seam: lets tests inject a fake FlowObserver without a live relay.
var newFlowObserver = func(serverAddress string) hubble.FlowObserver {
	return hubble.NewRelayObserver(serverAddress)
}

const networkCmdLong = `Inspect live network traffic flows via Cilium Hubble.

When the cluster uses the Cilium CNI, Cilium's eBPF datapath records every
network flow and exposes them through the Hubble Relay API. This command
queries the most recent flows and prints them, so you can debug connectivity,
spot drops, and see which workloads talk to each other without leaving the
terminal.

The command connects to a Hubble Relay address (default ` + hubble.DefaultRelayAddress + `).
Port-forward the in-cluster relay first, e.g.:

  kubectl -n kube-system port-forward svc/hubble-relay 4245:80

Pass --follow to keep streaming live flows as they happen (like 'kubectl logs
-f'), until you interrupt with Ctrl-C.

Output formats:
  - plain: an aligned table (default; one row per flow while following)
  - json:  a JSON array of flow records, or newline-delimited JSON (one object
           per line) while following — both fit scripting / AI consumption

Examples:
  # Show the 20 most recent flows
  ksail workload network

  # Only flows in the "default" namespace, as JSON
  ksail workload network --namespace default --output json

  # Only the last 100 TCP flows touching a pod
  ksail workload network --pod my-app --protocol tcp --last 100

  # Stream live flows until Ctrl-C
  ksail workload network --follow`

// NewNetworkCmd creates the command that streams Cilium Hubble traffic flows.
func NewNetworkCmd() *cobra.Command {
	var (
		namespace string
		pod       string
		protocol  string
		output    string
		server    string
		last      uint
		follow    bool
	)

	cmd := &cobra.Command{
		Use:          "network",
		Short:        "Inspect live network traffic flows via Cilium Hubble",
		Long:         networkCmdLong,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
	}

	cfgManager := createNetworkConfigManager(cmd)

	cmd.Flags().
		StringVarP(&namespace, "namespace", "n", "", "Filter flows by namespace (source or destination)")
	cmd.Flags().
		StringVar(&pod, "pod", "", "Filter flows by pod-name substring (source or destination)")
	cmd.Flags().
		StringVar(&protocol, "protocol", "", "Filter flows by L4 protocol (TCP, UDP, ICMPv4, ICMPv6, SCTP)")
	cmd.Flags().StringVarP(&output, "output", "o", hubble.OutputPlain, "Output format: plain, json")
	cmd.Flags().
		StringVar(&server, "server", hubble.DefaultRelayAddress, "Hubble Relay address to query")
	cmd.Flags().UintVar(&last, "last", defaultFlowCount, "Number of most recent flows to fetch")
	cmd.Flags().
		BoolVarP(&follow, "follow", "f", false, "Stream flows continuously until interrupted (Ctrl-C)")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runNetworkCommand(cmd, cfgManager, hubble.Options{
			Last:   uint64(last),
			Output: output,
			Follow: follow,
			Filter: hubble.FilterOptions{
				Namespace: namespace,
				Pod:       pod,
				Protocol:  protocol,
			},
		}, server)
	}

	return cmd
}

// createNetworkConfigManager builds a config manager that loads just enough of
// the cluster config to confirm the active CNI.
func createNetworkConfigManager(cmd *cobra.Command) *configmanager.ConfigManager {
	fieldSelectors := []configmanager.FieldSelector[v1alpha1.Cluster]{
		configmanager.DefaultCNIFieldSelector(),
	}

	return configmanager.NewCommandConfigManager(cmd, fieldSelectors)
}

func runNetworkCommand(
	cmd *cobra.Command,
	cfgManager *configmanager.ConfigManager,
	opts hubble.Options,
	server string,
) error {
	clusterCfg, err := cfgManager.Load(configmanagerinterface.LoadOptions{
		Silent:         true,
		SkipValidation: true,
	})
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if clusterCfg.Spec.Cluster.CNI != v1alpha1.CNICilium {
		return ErrCNINotCilium
	}

	observer := newFlowObserver(server)

	if opts.Follow {
		err = hubble.Stream(cmd.Context(), observer, opts, cmd.OutOrStdout())
		if err != nil {
			return fmt.Errorf("stream network flows: %w", err)
		}

		return nil
	}

	err = hubble.Observe(cmd.Context(), observer, opts, cmd.OutOrStdout())
	if err != nil {
		return fmt.Errorf("observe network flows: %w", err)
	}

	return nil
}
