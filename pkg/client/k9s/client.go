package k9s

import (
	"os"

	k9scmd "github.com/derailed/k9s/cmd"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui"
	"github.com/spf13/cobra"
)

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
	// Set terminal title to "ksail" before launching k9s
	ui.SetTerminalTitle("ksail")

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

	// Execute k9s.
	k9scmd.Execute()

	return nil
}
