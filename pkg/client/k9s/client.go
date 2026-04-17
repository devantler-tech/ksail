package k9s

import (
	"flag"
	"os"
	"sync"

	k9scmd "github.com/derailed/k9s/cmd"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

// silenceKlogOnce ensures klog is configured at most once per process.
var silenceKlogOnce sync.Once

// silenceKlog redirects klog output away from stderr so client-go log lines
// (e.g. reflector "Failed to watch" errors) do not corrupt the k9s TUI.
//
// The upstream k9s binary performs the same setup from its own main.go
// init(). Because ksail embeds only the k9s cmd/ subpackage, that init is
// never executed and klog writes directly to stderr while the alternate
// screen is active, producing the garbled output reported in
// `ksail cluster connect`.
//
// See github.com/derailed/k9s/main.go for the reference implementation.
func silenceKlog() {
	silenceKlogOnce.Do(func() {
		// klog.InitFlags panics if its flags are already registered on the
		// default flag.CommandLine (e.g. by another dependency). Only bind
		// klog's flags if they aren't already present.
		if flag.Lookup("logtostderr") == nil {
			klog.InitFlags(nil)
		}

		for name, value := range map[string]string{
			"logtostderr":     "false",
			"alsologtostderr": "false",
			"stderrthreshold": "fatal",
			"v":               "-10",
		} {
			// Errors here would only occur if klog's flag names change upstream.
			// Ignore to keep the TUI launch path side-effect free.
			_ = flag.Set(name, value)
		}
	})
}

// Client wraps k9s command functionality.
type Client struct{}

// NewClient creates a new k9s client instance with the default executor.
func NewClient() *Client {
	return &Client{}
}

// CreateConnectCommand creates a k9s command with all its flags and behavior.
func (c *Client) CreateConnectCommand(kubeConfigPath, context string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect to cluster with k9s",
		Long:  "Launch k9s terminal UI to interactively manage your Kubernetes cluster.",
		RunE: func(_ *cobra.Command, args []string) error {
			return c.runK9s(kubeConfigPath, context, args)
		},
		SilenceUsage: true,
	}

	return cmd
}

func (c *Client) runK9s(kubeConfigPath, context string, args []string) error {
	// Set up os.Args to pass flags to k9s
	originalArgs := os.Args

	defer func() {
		os.Args = originalArgs
	}()

	// Build arguments for k9s
	k9sArgs := []string{"k9s"}

	// Add kubeconfig flag if provided
	if kubeConfigPath != "" {
		k9sArgs = append(k9sArgs, "--kubeconfig", kubeConfigPath)
	}

	// Add context flag if provided
	if context != "" {
		k9sArgs = append(k9sArgs, "--context", context)
	}

	// Append all additional arguments passed by user
	k9sArgs = append(k9sArgs, args...)

	// Set os.Args for k9s to parse
	os.Args = k9sArgs

	// Prevent client-go klog messages from leaking onto the TUI.
	silenceKlog()

	// Execute k9s.
	k9scmd.Execute()

	return nil
}
