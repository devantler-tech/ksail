package kubectl

import (
	"errors"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

var (
	// ErrResourceCommandNotFound is returned when a kubectl create subcommand is not found.
	ErrResourceCommandNotFound = errors.New("kubectl create command not found for resource type")
	// ErrNoRunFunction is returned when a kubectl command has neither RunE nor Run function.
	ErrNoRunFunction = errors.New("no run function found for kubectl create command")
)

// Client wraps kubectl command functionality.
type Client struct {
	ioStreams genericiooptions.IOStreams
}

// NewClient creates a new kubectl client instance.
func NewClient(streams genericiooptions.IOStreams) *Client {
	client := &Client{}
	client.ioStreams = streams

	return client
}

// NewClientWithStdio returns a kubectl client wired to the default stdio streams.
func NewClientWithStdio() *Client {
	return NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	})
}

// Factory and command customization helpers.

// createFactory creates a kubectl factory with the given kubeconfig path.
// It returns both the factory and config flags so callers can register
// the flags (--namespace, --context, etc.) on their Cobra commands.
func (c *Client) createFactory(
	kubeConfigPath string,
) (cmdutil.Factory, *genericclioptions.ConfigFlags) {
	configFlags := genericclioptions.NewConfigFlags(true)
	if kubeConfigPath != "" {
		configFlags.KubeConfig = &kubeConfigPath
	}

	matchVersionKubeConfigFlags := cmdutil.NewMatchVersionFlags(configFlags)

	return cmdutil.NewFactory(matchVersionKubeConfigFlags), configFlags
}

// customizeCommand applies standard customizations to a kubectl command.
// When configFlags is non-nil, it registers --namespace, --context, and other
// kubectl config flags on the command so they can be used from the CLI.
func (c *Client) customizeCommand(
	cmd *cobra.Command,
	use, short, long string,
	configFlags *genericclioptions.ConfigFlags,
) {
	cmd.Use = use
	cmd.Short = short
	cmd.Long = long
	replaceKubectlInExamples(cmd)

	if configFlags != nil {
		configFlags.AddFlags(cmd.Flags())
	}
}

// replaceKubectlInExamples replaces "kubectl" with "ksail workload" in command examples.
func replaceKubectlInExamples(cmd *cobra.Command) {
	if cmd.Example != "" {
		cmd.Example = strings.ReplaceAll(cmd.Example, "kubectl", "ksail workload")
	}
}
