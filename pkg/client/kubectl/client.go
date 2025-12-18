package kubectl

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/cmd/apply"
	"k8s.io/kubectl/pkg/cmd/clusterinfo"
	"k8s.io/kubectl/pkg/cmd/create"
	"k8s.io/kubectl/pkg/cmd/delete"
	"k8s.io/kubectl/pkg/cmd/describe"
	"k8s.io/kubectl/pkg/cmd/edit"
	"k8s.io/kubectl/pkg/cmd/exec"
	"k8s.io/kubectl/pkg/cmd/explain"
	"k8s.io/kubectl/pkg/cmd/expose"
	"k8s.io/kubectl/pkg/cmd/get"
	"k8s.io/kubectl/pkg/cmd/logs"
	"k8s.io/kubectl/pkg/cmd/rollout"
	"k8s.io/kubectl/pkg/cmd/scale"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/cmd/wait"
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

// CreateApplyCommand creates a kubectl apply command with all its flags and behavior.
func (c *Client) CreateApplyCommand(kubeConfigPath string) *cobra.Command {
	factory := c.createFactory(kubeConfigPath)
	applyCmd := apply.NewCmdApply("ksail workload", factory, c.ioStreams)

	c.customizeCommand(
		applyCmd,
		"apply",
		"Apply manifests",
		"Apply local Kubernetes manifests to your cluster.",
	)

	return applyCmd
}

// CreateCreateCommand creates a kubectl create command with all its flags and behavior.
func (c *Client) CreateCreateCommand(kubeConfigPath string) *cobra.Command {
	factory := c.createFactory(kubeConfigPath)
	createCmd := create.NewCmdCreate(factory, c.ioStreams)

	c.customizeCommand(
		createCmd,
		"create",
		"Create resources",
		"Create Kubernetes resources from files or stdin.",
	)

	return createCmd
}

// CreateEditCommand creates a kubectl edit command with all its flags and behavior.
func (c *Client) CreateEditCommand(kubeConfigPath string) *cobra.Command {
	factory := c.createFactory(kubeConfigPath)
	editCmd := edit.NewCmdEdit(factory, c.ioStreams)

	c.customizeCommand(
		editCmd,
		"edit",
		"Edit a resource",
		"Edit a Kubernetes resource from the default editor.",
	)

	return editCmd
}

// CreateDeleteCommand creates a kubectl delete command with all its flags and behavior.
func (c *Client) CreateDeleteCommand(kubeConfigPath string) *cobra.Command {
	factory := c.createFactory(kubeConfigPath)
	deleteCmd := delete.NewCmdDelete(factory, c.ioStreams)

	c.customizeCommand(
		deleteCmd,
		"delete",
		"Delete resources",
		"Delete Kubernetes resources by file names, stdin, resources and names, or by resources and label selector.",
	)

	return deleteCmd
}

// CreateDescribeCommand creates a kubectl describe command with all its flags and behavior.
func (c *Client) CreateDescribeCommand(kubeConfigPath string) *cobra.Command {
	factory := c.createFactory(kubeConfigPath)
	describeCmd := describe.NewCmdDescribe("ksail workload", factory, c.ioStreams)

	c.customizeCommand(
		describeCmd,
		"describe",
		"Describe resources",
		"Show details of a specific resource or group of resources.",
	)

	return describeCmd
}

// CreateExplainCommand creates a kubectl explain command with all its flags and behavior.
func (c *Client) CreateExplainCommand(kubeConfigPath string) *cobra.Command {
	factory := c.createFactory(kubeConfigPath)
	explainCmd := explain.NewCmdExplain("ksail workload", factory, c.ioStreams)

	c.customizeCommand(
		explainCmd,
		"explain",
		"Get documentation for a resource",
		"Get documentation for Kubernetes resources, including field descriptions and structure.",
	)

	return explainCmd
}

// CreateGetCommand creates a kubectl get command with all its flags and behavior.
func (c *Client) CreateGetCommand(kubeConfigPath string) *cobra.Command {
	factory := c.createFactory(kubeConfigPath)
	getCmd := get.NewCmdGet("ksail workload", factory, c.ioStreams)

	c.customizeCommand(
		getCmd,
		"get",
		"Get resources",
		"Display one or many Kubernetes resources from your cluster.",
	)

	return getCmd
}

// CreateLogsCommand creates a kubectl logs command with all its flags and behavior.
func (c *Client) CreateLogsCommand(kubeConfigPath string) *cobra.Command {
	factory := c.createFactory(kubeConfigPath)
	logsCmd := logs.NewCmdLogs(factory, c.ioStreams)

	c.customizeCommand(
		logsCmd,
		"logs",
		"Print container logs",
		"Print the logs for a container in a pod or specified resource. "+
			"If the pod has only one container, the container name is optional.",
	)

	return logsCmd
}

// CreateRolloutCommand creates a kubectl rollout command with all its flags and behavior.
func (c *Client) CreateRolloutCommand(kubeConfigPath string) *cobra.Command {
	factory := c.createFactory(kubeConfigPath)
	rolloutCmd := rollout.NewCmdRollout(factory, c.ioStreams)

	c.customizeCommand(
		rolloutCmd,
		"rollout",
		"Manage the rollout of a resource",
		"Manage the rollout of one or many resources.",
	)

	return rolloutCmd
}

// CreateScaleCommand creates a kubectl scale command with all its flags and behavior.
func (c *Client) CreateScaleCommand(kubeConfigPath string) *cobra.Command {
	factory := c.createFactory(kubeConfigPath)
	scaleCmd := scale.NewCmdScale(factory, c.ioStreams)

	c.customizeCommand(
		scaleCmd,
		"scale",
		"Scale resources",
		"Set a new size for a deployment, replica set, replication controller, or stateful set.",
	)

	return scaleCmd
}

// CreateExposeCommand creates a kubectl expose command with all its flags and behavior.
func (c *Client) CreateExposeCommand(kubeConfigPath string) *cobra.Command {
	factory := c.createFactory(kubeConfigPath)
	exposeCmd := expose.NewCmdExposeService(factory, c.ioStreams)

	c.customizeCommand(
		exposeCmd,
		"expose",
		"Expose a resource as a service",
		"Expose a resource as a new Kubernetes service.",
	)

	return exposeCmd
}

// CreateClusterInfoCommand wires kubectl's cluster-info with minimal guarding.
func (c *Client) CreateClusterInfoCommand(kubeConfigPath string) *cobra.Command {
	configFlags := genericclioptions.NewConfigFlags(true)
	if kubeConfigPath != "" {
		configFlags.KubeConfig = &kubeConfigPath
	}

	restClientGetter := cmdutil.NewMatchVersionFlags(configFlags)
	options := &clusterinfo.ClusterInfoOptions{IOStreams: c.ioStreams}

	clusterInfoCmd := &cobra.Command{
		Use:   "info",
		Short: "Display cluster information",
		Long:  "Display addresses of the control plane and services with label kubernetes.io/cluster-service=true.",
		//nolint:noinlineerr // error handling in Cobra command
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := options.Complete(restClientGetter, cmd); err != nil {
				return fmt.Errorf("complete cluster-info options: %w", err)
			}

			// Ensure REST config has defaults (notably GroupVersion) to avoid nil deref in upstream logic.
			if options.Client != nil {
				if err := rest.SetKubernetesDefaults(options.Client); err != nil {
					return fmt.Errorf("set Kubernetes defaults: %w", err)
				}
			}

			return options.Run()
		},
	}

	configFlags.AddFlags(clusterInfoCmd.Flags())
	clusterInfoCmd.AddCommand(clusterinfo.NewCmdClusterInfoDump(restClientGetter, c.ioStreams))

	return clusterInfoCmd
}

// CreateExecCommand creates a kubectl exec command with all its flags and behavior.
func (c *Client) CreateExecCommand(kubeConfigPath string) *cobra.Command {
	factory := c.createFactory(kubeConfigPath)
	execCmd := exec.NewCmdExec(factory, c.ioStreams)

	c.customizeCommand(
		execCmd,
		"exec",
		"Execute a command in a container",
		"Execute a command in a container in a pod.",
	)

	return execCmd
}

// CreateWaitCommand creates a kubectl wait command with all its flags and behavior.
func (c *Client) CreateWaitCommand(kubeConfigPath string) *cobra.Command {
	// Create config flags with kubeconfig path
	configFlags := genericclioptions.NewConfigFlags(true)
	if kubeConfigPath != "" {
		configFlags.KubeConfig = &kubeConfigPath
	}

	// Create the wait command using kubectl's NewCmdWait
	waitCmd := wait.NewCmdWait(configFlags, c.ioStreams)

	// Customize command metadata to fit ksail context
	waitCmd.Use = "wait"
	waitCmd.Short = "Wait for a specific condition on one or many resources"
	waitCmd.Long = "Wait for a specific condition on one or many resources. " +
		"The command takes multiple resources and waits until the specified condition " +
		"is seen in the Status field of every given resource."
	replaceKubectlInExamples(waitCmd)

	return waitCmd
}

// CreateNamespaceCmd creates a Namespace manifest generator command using the client's IO streams.
func (c *Client) CreateNamespaceCmd() (*cobra.Command, error) {
	return c.newResourceCmd("namespace")
}

// CreateConfigMapCmd creates a ConfigMap manifest generator command using the client's IO streams.
func (c *Client) CreateConfigMapCmd() (*cobra.Command, error) {
	return c.newResourceCmd("configmap")
}

// CreateSecretCmd creates a Secret manifest generator command using the client's IO streams.
func (c *Client) CreateSecretCmd() (*cobra.Command, error) {
	return c.newResourceCmd("secret")
}

// CreateServiceAccountCmd creates a ServiceAccount manifest generator command using the client's IO streams.
func (c *Client) CreateServiceAccountCmd() (*cobra.Command, error) {
	return c.newResourceCmd("serviceaccount")
}

// CreateDeploymentCmd creates a Deployment manifest generator command using the client's IO streams.
func (c *Client) CreateDeploymentCmd() (*cobra.Command, error) {
	return c.newResourceCmd("deployment")
}

// CreateJobCmd creates a Job manifest generator command using the client's IO streams.
func (c *Client) CreateJobCmd() (*cobra.Command, error) {
	return c.newResourceCmd("job")
}

// CreateCronJobCmd creates a CronJob manifest generator command using the client's IO streams.
func (c *Client) CreateCronJobCmd() (*cobra.Command, error) {
	return c.newResourceCmd("cronjob")
}

// CreateServiceCmd creates a Service manifest generator command using the client's IO streams.
func (c *Client) CreateServiceCmd() (*cobra.Command, error) {
	return c.newResourceCmd("service")
}

// CreateIngressCmd creates an Ingress manifest generator command using the client's IO streams.
func (c *Client) CreateIngressCmd() (*cobra.Command, error) {
	return c.newResourceCmd("ingress")
}

// CreateRoleCmd creates a Role manifest generator command using the client's IO streams.
func (c *Client) CreateRoleCmd() (*cobra.Command, error) {
	return c.newResourceCmd("role")
}

// CreateRoleBindingCmd creates a RoleBinding manifest generator command using the client's IO streams.
func (c *Client) CreateRoleBindingCmd() (*cobra.Command, error) {
	return c.newResourceCmd("rolebinding")
}

// CreateClusterRoleCmd creates a ClusterRole manifest generator command using the client's IO streams.
func (c *Client) CreateClusterRoleCmd() (*cobra.Command, error) {
	return c.newResourceCmd("clusterrole")
}

// CreateClusterRoleBindingCmd creates a ClusterRoleBinding manifest generator command using the client's IO streams.
func (c *Client) CreateClusterRoleBindingCmd() (*cobra.Command, error) {
	return c.newResourceCmd("clusterrolebinding")
}

// CreateQuotaCmd creates a ResourceQuota manifest generator command using the client's IO streams.
func (c *Client) CreateQuotaCmd() (*cobra.Command, error) {
	return c.newResourceCmd("quota")
}

// CreatePodDisruptionBudgetCmd creates a PodDisruptionBudget manifest generator command using the client's IO streams.
func (c *Client) CreatePodDisruptionBudgetCmd() (*cobra.Command, error) {
	return c.newResourceCmd("poddisruptionbudget")
}

// CreatePriorityClassCmd creates a PriorityClass manifest generator command using the client's IO streams.
func (c *Client) CreatePriorityClassCmd() (*cobra.Command, error) {
	return c.newResourceCmd("priorityclass")
}

// Factory and command customization helpers.

// createFactory creates a kubectl factory with the given kubeconfig path.
func (c *Client) createFactory(kubeConfigPath string) cmdutil.Factory {
	configFlags := genericclioptions.NewConfigFlags(true)
	if kubeConfigPath != "" {
		configFlags.KubeConfig = &kubeConfigPath
	}

	matchVersionKubeConfigFlags := cmdutil.NewMatchVersionFlags(configFlags)

	return cmdutil.NewFactory(matchVersionKubeConfigFlags)
}

// customizeCommand applies standard customizations to a kubectl command.
func (c *Client) customizeCommand(cmd *cobra.Command, use, short, long string) {
	cmd.Use = use
	cmd.Short = short
	cmd.Long = long
	replaceKubectlInExamples(cmd)
}

// replaceKubectlInExamples replaces "kubectl" with "ksail workload" in command examples.
func replaceKubectlInExamples(cmd *cobra.Command) {
	if cmd.Example != "" {
		cmd.Example = strings.ReplaceAll(cmd.Example, "kubectl", "ksail workload")
	}
}

// newResourceCmd creates a gen command that wraps kubectl create with forced --dry-run=client -o yaml.
func (c *Client) newResourceCmd(resourceType string) (*cobra.Command, error) {
	// Use empty string for kubeconfig since --dry-run=client doesn't need cluster access
	tempCreateCmd := c.CreateCreateCommand("")

	// Find the subcommand for this resource type
	var resourceCmd *cobra.Command

	for _, subCmd := range tempCreateCmd.Commands() {
		if subCmd.Name() == resourceType {
			resourceCmd = subCmd

			break
		}
	}

	if resourceCmd == nil {
		return nil, fmt.Errorf("%w: %s", ErrResourceCommandNotFound, resourceType)
	}

	// Create a wrapper command
	wrapperCmd := &cobra.Command{
		Use:          resourceCmd.Use,
		Short:        resourceCmd.Short,
		Long:         resourceCmd.Long,
		Example:      resourceCmd.Example,
		Aliases:      resourceCmd.Aliases,
		SilenceUsage: true,
	}

	// Set default output to client streams for standalone usage
	// When added as subcommand to another command, this can be overridden by parent
	wrapperCmd.SetOut(c.ioStreams.Out)
	wrapperCmd.SetErr(c.ioStreams.ErrOut)

	// If the resource has subcommands (like secret/service), recursively copy them
	if len(resourceCmd.Commands()) > 0 {
		for _, subCmd := range resourceCmd.Commands() {
			subWrapper := c.createSubcommandWrapper(resourceType, subCmd)
			wrapperCmd.AddCommand(subWrapper)
		}
	} else {
		// Create our custom RunE that calls kubectl with forced flags
		wrapperCmd.RunE = func(cmd *cobra.Command, args []string) error {
			return c.executeResourceGen(resourceType, cmd, args)
		}

		// Copy all flags from the resource command
		wrapperCmd.Flags().AddFlagSet(resourceCmd.Flags())
	}

	return wrapperCmd, nil
}

// createSubcommandWrapper creates a wrapper for a subcommand (e.g., secret generic).
func (c *Client) createSubcommandWrapper(parentType string, subCmd *cobra.Command) *cobra.Command {
	wrapper := &cobra.Command{
		Use:          subCmd.Use,
		Short:        subCmd.Short,
		Long:         subCmd.Long,
		Example:      subCmd.Example,
		Aliases:      subCmd.Aliases,
		SilenceUsage: true,
	}

	// Don't set output here - subcommand wrappers inherit from parent command
	// This allows tests to call SetOut() on parent and have it propagate

	// Create RunE for the subcommand
	wrapper.RunE = func(cmd *cobra.Command, args []string) error {
		return c.executeSubcommandGen(parentType, subCmd.Name(), cmd, args)
	}

	// Copy all flags from the subcommand
	wrapper.Flags().AddFlagSet(subCmd.Flags())

	return wrapper
}

// executeSubcommandGen executes kubectl create with subcommand and forced flags.
func (c *Client) executeSubcommandGen(
	parentType, subType string,
	cmd *cobra.Command,
	args []string,
) error {
	// Create a fresh client with the command's IO streams to ensure output goes to the right place
	freshClient := NewClient(genericiooptions.IOStreams{
		In:     cmd.InOrStdin(),
		Out:    cmd.OutOrStdout(),
		ErrOut: cmd.ErrOrStderr(),
	})

	// Create a fresh kubectl create command
	createCmd := freshClient.CreateCreateCommand("")

	// Find the parent resource command
	var parentCmd *cobra.Command

	for _, subCmd := range createCmd.Commands() {
		if subCmd.Name() == parentType {
			parentCmd = subCmd

			break
		}
	}

	if parentCmd == nil {
		return fmt.Errorf("%w: %s", ErrResourceCommandNotFound, parentType)
	}

	// Find the subcommand
	var freshSubCmd *cobra.Command

	for _, sub := range parentCmd.Commands() {
		if sub.Name() == subType {
			freshSubCmd = sub

			break
		}
	}

	if freshSubCmd == nil {
		return fmt.Errorf("%w: %s %s", ErrResourceCommandNotFound, parentType, subType)
	}

	// Force --dry-run=client and -o yaml
	err := freshClient.setForcedFlags(freshSubCmd)
	if err != nil {
		return err
	}

	// Ensure command output is captured by the wrapper command
	freshSubCmd.SetOut(cmd.OutOrStdout())
	freshSubCmd.SetErr(cmd.ErrOrStderr())

	// Copy user flags
	err = freshClient.copyUserFlags(cmd, freshSubCmd)
	if err != nil {
		return err
	}

	// Execute
	return freshClient.executeCommand(freshSubCmd, args)
}

// executeResourceGen executes kubectl create with forced --dry-run=client -o yaml flags.
func (c *Client) executeResourceGen(resourceType string, cmd *cobra.Command, args []string) error {
	// Create a fresh client with the command's IO streams to ensure output goes to the right place
	freshClient := NewClient(genericiooptions.IOStreams{
		In:     cmd.InOrStdin(),
		Out:    cmd.OutOrStdout(),
		ErrOut: cmd.ErrOrStderr(),
	})

	// Create a fresh kubectl create command
	createCmd := freshClient.CreateCreateCommand("")

	// Find the resource command
	freshResourceCmd := freshClient.findResourceCommand(createCmd, resourceType)
	if freshResourceCmd == nil {
		return fmt.Errorf("%w: %s", ErrResourceCommandNotFound, resourceType)
	}

	// Force --dry-run=client and -o yaml FIRST
	err := freshClient.setForcedFlags(freshResourceCmd)
	if err != nil {
		return err
	}

	// Copy user flags
	err = freshClient.copyUserFlags(cmd, freshResourceCmd)
	if err != nil {
		return err
	}

	// Execute
	return freshClient.executeCommand(freshResourceCmd, args)
}

// findResourceCommand finds a kubectl create subcommand by resource type name.
func (c *Client) findResourceCommand(createCmd *cobra.Command, resourceType string) *cobra.Command {
	for _, subCmd := range createCmd.Commands() {
		if subCmd.Name() == resourceType {
			return subCmd
		}
	}

	return nil
}

// setForcedFlags sets the --dry-run=client and -o yaml flags.
func (c *Client) setForcedFlags(cmd *cobra.Command) error {
	err := cmd.Flags().Set("dry-run", "client")
	if err != nil {
		return fmt.Errorf("failed to set dry-run flag: %w", err)
	}

	err = cmd.Flags().Set("output", "yaml")
	if err != nil {
		return fmt.Errorf("failed to set output flag: %w", err)
	}

	return nil
}

// copyUserFlags copies user-provided flags from wrapper command to kubectl command.
func (c *Client) copyUserFlags(wrapperCmd, targetCmd *cobra.Command) error {
	var errs []error

	wrapperCmd.Flags().Visit(func(flag *pflag.Flag) {
		if flag.Name == "dry-run" || flag.Name == "output" {
			return
		}

		targetFlag := targetCmd.Flags().Lookup(flag.Name)
		if targetFlag != nil {
			err := c.copyFlagValue(flag, targetCmd)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to copy flag %s: %w", flag.Name, err))
			}
		}
	})

	if len(errs) > 0 {
		return fmt.Errorf("failed to copy flags: %w", errors.Join(errs...))
	}

	return nil
}

// copyFlagValue copies a flag value, handling slice flags specially.
func (c *Client) copyFlagValue(flag *pflag.Flag, targetCmd *cobra.Command) error {
	// For slice flags, we need to get the actual slice values
	if sliceVal, ok := flag.Value.(pflag.SliceValue); ok {
		strSlice := sliceVal.GetSlice()
		for _, v := range strSlice {
			err := targetCmd.Flags().Set(flag.Name, v)
			if err != nil {
				return fmt.Errorf("failed to set flag %s: %w", flag.Name, err)
			}
		}
	} else {
		// For non-slice flags, just copy the string value
		err := targetCmd.Flags().Set(flag.Name, flag.Value.String())
		if err != nil {
			return fmt.Errorf("failed to set flag %s: %w", flag.Name, err)
		}
	}

	return nil
}

// executeCommand executes the kubectl command's Run or RunE function.
func (c *Client) executeCommand(cmd *cobra.Command, args []string) error {
	if cmd.RunE != nil {
		err := cmd.RunE(cmd, args)
		if err != nil {
			return fmt.Errorf("kubectl command execution failed: %w", err)
		}

		return nil
	}

	if cmd.Run != nil {
		cmd.Run(cmd, args)

		return nil
	}

	return ErrNoRunFunction
}
