package kubectl

import (
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
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

// CreateApplyCommand creates a kubectl apply command with all its flags and behavior.
func (c *Client) CreateApplyCommand(kubeConfigPath string) *cobra.Command {
	factory, configFlags := c.createFactory(kubeConfigPath)
	applyCmd := apply.NewCmdApply("ksail workload", factory, c.ioStreams)

	c.customizeCommand(
		applyCmd,
		"apply",
		"Apply manifests",
		"Apply local Kubernetes manifests to your cluster.",
		configFlags,
	)

	return applyCmd
}

// CreateCreateCommand creates a kubectl create command with all its flags and behavior.
func (c *Client) CreateCreateCommand(kubeConfigPath string) *cobra.Command {
	factory, configFlags := c.createFactory(kubeConfigPath)
	createCmd := create.NewCmdCreate(factory, c.ioStreams)

	c.customizeCommand(
		createCmd,
		"create",
		"Create resources",
		"Create Kubernetes resources from files or stdin.",
		configFlags,
	)

	return createCmd
}

// CreateEditCommand creates a kubectl edit command with all its flags and behavior.
func (c *Client) CreateEditCommand(kubeConfigPath string) *cobra.Command {
	factory, configFlags := c.createFactory(kubeConfigPath)
	editCmd := edit.NewCmdEdit(factory, c.ioStreams)

	c.customizeCommand(
		editCmd,
		"edit",
		"Edit a resource",
		"Edit a Kubernetes resource from the default editor.",
		configFlags,
	)

	return editCmd
}

// CreateDeleteCommand creates a kubectl delete command with all its flags and behavior.
func (c *Client) CreateDeleteCommand(kubeConfigPath string) *cobra.Command {
	factory, configFlags := c.createFactory(kubeConfigPath)
	deleteCmd := delete.NewCmdDelete(factory, c.ioStreams)

	c.customizeCommand(
		deleteCmd,
		"delete",
		"Delete resources",
		"Delete Kubernetes resources by file names, stdin, resources and names, or by resources and label selector.",
		configFlags,
	)

	return deleteCmd
}

// CreateDescribeCommand creates a kubectl describe command with all its flags and behavior.
func (c *Client) CreateDescribeCommand(kubeConfigPath string) *cobra.Command {
	factory, configFlags := c.createFactory(kubeConfigPath)
	describeCmd := describe.NewCmdDescribe("ksail workload", factory, c.ioStreams)

	c.customizeCommand(
		describeCmd,
		"describe",
		"Describe resources",
		"Show details of a specific resource or group of resources.",
		configFlags,
	)

	return describeCmd
}

// CreateExplainCommand creates a kubectl explain command with all its flags and behavior.
func (c *Client) CreateExplainCommand(kubeConfigPath string) *cobra.Command {
	factory, configFlags := c.createFactory(kubeConfigPath)
	explainCmd := explain.NewCmdExplain("ksail workload", factory, c.ioStreams)

	c.customizeCommand(
		explainCmd,
		"explain",
		"Get documentation for a resource",
		"Get documentation for Kubernetes resources, including field descriptions and structure.",
		configFlags,
	)

	return explainCmd
}

// CreateGetCommand creates a kubectl get command with all its flags and behavior.
func (c *Client) CreateGetCommand(kubeConfigPath string) *cobra.Command {
	factory, configFlags := c.createFactory(kubeConfigPath)
	getCmd := get.NewCmdGet("ksail workload", factory, c.ioStreams)

	c.customizeCommand(
		getCmd,
		"get",
		"Get resources",
		"Display one or many Kubernetes resources from your cluster.",
		configFlags,
	)

	return getCmd
}

// CreateLogsCommand creates a kubectl logs command with all its flags and behavior.
func (c *Client) CreateLogsCommand(kubeConfigPath string) *cobra.Command {
	factory, configFlags := c.createFactory(kubeConfigPath)
	logsCmd := logs.NewCmdLogs(factory, c.ioStreams)

	c.customizeCommand(
		logsCmd,
		"logs",
		"Print container logs",
		"Print the logs for a container in a pod or specified resource. "+
			"If the pod has only one container, the container name is optional.",
		configFlags,
	)

	return logsCmd
}

// CreateRolloutCommand creates a kubectl rollout command with all its flags and behavior.
func (c *Client) CreateRolloutCommand(kubeConfigPath string) *cobra.Command {
	factory, configFlags := c.createFactory(kubeConfigPath)
	rolloutCmd := rollout.NewCmdRollout(factory, c.ioStreams)

	c.customizeCommand(
		rolloutCmd,
		"rollout",
		"Manage the rollout of a resource",
		"Manage the rollout of one or many resources.",
		configFlags,
	)

	return rolloutCmd
}

// CreateScaleCommand creates a kubectl scale command with all its flags and behavior.
func (c *Client) CreateScaleCommand(kubeConfigPath string) *cobra.Command {
	factory, configFlags := c.createFactory(kubeConfigPath)
	scaleCmd := scale.NewCmdScale(factory, c.ioStreams)

	c.customizeCommand(
		scaleCmd,
		"scale",
		"Scale resources",
		"Set a new size for a deployment, replica set, replication controller, or stateful set.",
		configFlags,
	)

	return scaleCmd
}

// CreateExposeCommand creates a kubectl expose command with all its flags and behavior.
func (c *Client) CreateExposeCommand(kubeConfigPath string) *cobra.Command {
	factory, configFlags := c.createFactory(kubeConfigPath)
	exposeCmd := expose.NewCmdExposeService(factory, c.ioStreams)

	c.customizeCommand(
		exposeCmd,
		"expose",
		"Expose a resource as a service",
		"Expose a resource as a new Kubernetes service.",
		configFlags,
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
	factory, configFlags := c.createFactory(kubeConfigPath)
	execCmd := exec.NewCmdExec(factory, c.ioStreams)

	c.customizeCommand(
		execCmd,
		"exec",
		"Execute a command in a container",
		"Execute a command in a container in a pod.",
		configFlags,
	)

	return execCmd
}

// CreateWaitCommand creates a kubectl wait command with all its flags and behavior.
func (c *Client) CreateWaitCommand(kubeConfigPath string) *cobra.Command {
	configFlags := genericclioptions.NewConfigFlags(true)
	if kubeConfigPath != "" {
		configFlags.KubeConfig = &kubeConfigPath
	}

	waitCmd := wait.NewCmdWait(configFlags, c.ioStreams)

	waitCmd.Use = "wait"
	waitCmd.Short = "Wait for a specific condition on one or many resources"
	waitCmd.Long = "Wait for a specific condition on one or many resources. " +
		"The command takes multiple resources and waits until the specified condition " +
		"is seen in the Status field of every given resource."
	replaceKubectlInExamples(waitCmd)

	return waitCmd
}
