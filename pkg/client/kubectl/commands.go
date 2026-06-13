package kubectl

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/cmd/apply"
	"k8s.io/kubectl/pkg/cmd/clusterinfo"
	"k8s.io/kubectl/pkg/cmd/create"
	"k8s.io/kubectl/pkg/cmd/debug"
	"k8s.io/kubectl/pkg/cmd/delete"
	"k8s.io/kubectl/pkg/cmd/describe"
	"k8s.io/kubectl/pkg/cmd/edit"
	"k8s.io/kubectl/pkg/cmd/exec"
	"k8s.io/kubectl/pkg/cmd/explain"
	"k8s.io/kubectl/pkg/cmd/expose"
	"k8s.io/kubectl/pkg/cmd/get"
	"k8s.io/kubectl/pkg/cmd/logs"
	"k8s.io/kubectl/pkg/cmd/portforward"
	"k8s.io/kubectl/pkg/cmd/rollout"
	"k8s.io/kubectl/pkg/cmd/scale"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/cmd/wait"
)

// commandSpec describes one uniform kubectl wrapper command. The build function
// constructs the upstream kubectl command from a factory and IO streams; use,
// short, and long carry the KSail-facing help strings; postCustomize, when set,
// runs after customizeCommand to apply per-command tweaks.
type commandSpec struct {
	use   string
	short string
	long  string

	build         func(cmdutil.Factory, genericiooptions.IOStreams) *cobra.Command
	postCustomize func(*cobra.Command)
}

// newWrappedCommand builds a kubectl command from the spec, applying the shared
// lock, factory, and customization uniformly. The fatalMu read lock guards
// against data races with withSafeFatal's writes to kubectl's global
// fatalErrHandler.
func (c *Client) newWrappedCommand(spec commandSpec, kubeConfigPath string) *cobra.Command {
	fatalMu.RLock()
	defer fatalMu.RUnlock()

	factory, configFlags := c.createFactory(kubeConfigPath)
	cmd := spec.build(factory, c.ioStreams)

	c.customizeCommand(cmd, spec.use, spec.short, spec.long, configFlags)

	if spec.postCustomize != nil {
		spec.postCustomize(cmd)
	}

	return cmd
}

// CreateApplyCommand creates a kubectl apply command with all its flags and behavior.
func (c *Client) CreateApplyCommand(kubeConfigPath string) *cobra.Command {
	return c.newWrappedCommand(commandSpec{
		use:   "apply",
		short: "Apply manifests",
		long:  "Apply local Kubernetes manifests to your cluster.",
		build: func(f cmdutil.Factory, s genericiooptions.IOStreams) *cobra.Command {
			return apply.NewCmdApply("ksail workload", f, s)
		},
	}, kubeConfigPath)
}

// CreateCreateCommand creates a kubectl create command with all its flags and behavior.
func (c *Client) CreateCreateCommand(kubeConfigPath string) *cobra.Command {
	return c.newWrappedCommand(commandSpec{
		use:   "create",
		short: "Create resources",
		long:  "Create Kubernetes resources from files or stdin.",
		build: create.NewCmdCreate,
	}, kubeConfigPath)
}

// CreateEditCommand creates a kubectl edit command with all its flags and behavior.
func (c *Client) CreateEditCommand(kubeConfigPath string) *cobra.Command {
	return c.newWrappedCommand(commandSpec{
		use:   "edit",
		short: "Edit a resource",
		long:  "Edit a Kubernetes resource from the default editor.",
		build: edit.NewCmdEdit,
	}, kubeConfigPath)
}

// CreateDeleteCommand creates a kubectl delete command with all its flags and behavior.
func (c *Client) CreateDeleteCommand(kubeConfigPath string) *cobra.Command {
	return c.newWrappedCommand(commandSpec{
		use:   "delete",
		short: "Delete resources",
		long:  "Delete Kubernetes resources by file names, stdin, resources and names, or by resources and label selector.",
		build: delete.NewCmdDelete,
	}, kubeConfigPath)
}

// CreateDescribeCommand creates a kubectl describe command with all its flags and behavior.
func (c *Client) CreateDescribeCommand(kubeConfigPath string) *cobra.Command {
	return c.newWrappedCommand(commandSpec{
		use:   "describe",
		short: "Describe resources",
		long: "Show details of a specific resource or group of resources, " +
			"including metadata, status conditions, events, and container info. " +
			"Useful for diagnosing why a resource is not ready — check the Events " +
			"and Conditions sections. " +
			"For Flux resources (kustomization, helmrelease), shows reconciliation " +
			"status and last error. " +
			"For ArgoCD resources (application), shows sync status and health.",
		build: func(f cmdutil.Factory, s genericiooptions.IOStreams) *cobra.Command {
			return describe.NewCmdDescribe("ksail workload", f, s)
		},
	}, kubeConfigPath)
}

// CreateExplainCommand creates a kubectl explain command with all its flags and behavior.
func (c *Client) CreateExplainCommand(kubeConfigPath string) *cobra.Command {
	return c.newWrappedCommand(commandSpec{
		use:   "explain",
		short: "Get documentation for a resource",
		long:  "Get documentation for Kubernetes resources, including field descriptions and structure.",
		build: func(f cmdutil.Factory, s genericiooptions.IOStreams) *cobra.Command {
			return explain.NewCmdExplain("ksail workload", f, s)
		},
	}, kubeConfigPath)
}

// CreateGetCommand creates a kubectl get command with all its flags and behavior.
func (c *Client) CreateGetCommand(kubeConfigPath string) *cobra.Command {
	return c.newWrappedCommand(commandSpec{
		use:   "get",
		short: "Get resources",
		long: "Display one or many Kubernetes resources from your cluster. " +
			"Use -o json for structured output that includes status conditions and health. " +
			"Use -o wide for additional columns. " +
			"Use --all-namespaces or -A to query across all namespaces. " +
			"Supports comma-separated resource types (e.g. deployments,services). " +
			"For GitOps diagnostics, query Flux resources (kustomization, helmrelease, gitrepository, ocirepository) " +
			"or ArgoCD resources (application) to check reconciliation status and errors.",
		build: func(f cmdutil.Factory, s genericiooptions.IOStreams) *cobra.Command {
			return get.NewCmdGet("ksail workload", f, s)
		},
	}, kubeConfigPath)
}

// CreateLogsCommand creates a kubectl logs command with all its flags and behavior.
func (c *Client) CreateLogsCommand(kubeConfigPath string) *cobra.Command {
	return c.newWrappedCommand(commandSpec{
		use:   "logs",
		short: "Print container logs",
		long: "Print the logs for a container in a pod or specified resource. " +
			"If the pod has only one container, the container name is optional. " +
			"Use --tail=N to limit output to the last N lines. " +
			"Use --previous to get logs from a previous container instance (useful for crash-loop diagnostics). " +
			"Use --all-containers to get logs from all containers in a pod. " +
			"For GitOps controller logs, query the controller pods directly " +
			"(e.g. in flux-system or argocd namespace).",
		build: logs.NewCmdLogs,
	}, kubeConfigPath)
}

// CreateRolloutCommand creates a kubectl rollout command with all its flags and behavior.
func (c *Client) CreateRolloutCommand(kubeConfigPath string) *cobra.Command {
	return c.newWrappedCommand(commandSpec{
		use:   "rollout",
		short: "Manage the rollout of a resource",
		long:  "Manage the rollout of one or many resources.",
		build: func(f cmdutil.Factory, s genericiooptions.IOStreams) *cobra.Command {
			return rollout.NewCmdRollout("ksail workload", f, s)
		},
	}, kubeConfigPath)
}

// CreateScaleCommand creates a kubectl scale command with all its flags and behavior.
func (c *Client) CreateScaleCommand(kubeConfigPath string) *cobra.Command {
	return c.newWrappedCommand(commandSpec{
		use:   "scale",
		short: "Scale resources",
		long:  "Set a new size for a deployment, replica set, replication controller, or stateful set.",
		build: scale.NewCmdScale,
	}, kubeConfigPath)
}

// CreateExposeCommand creates a kubectl expose command with all its flags and behavior.
func (c *Client) CreateExposeCommand(kubeConfigPath string) *cobra.Command {
	return c.newWrappedCommand(commandSpec{
		use:   "expose",
		short: "Expose a resource as a service",
		long:  "Expose a resource as a new Kubernetes service.",
		build: expose.NewCmdExposeService,
	}, kubeConfigPath)
}

// CreateDebugCommand creates a kubectl debug command with all its flags and behavior.
func (c *Client) CreateDebugCommand(kubeConfigPath string) *cobra.Command {
	return c.newWrappedCommand(commandSpec{
		use:   "debug",
		short: "Create debugging sessions for troubleshooting workloads and nodes",
		long: "Create debugging sessions for troubleshooting workloads and nodes.\n\n" +
			"Debug containers allow you to interactively troubleshoot running pods, " +
			"create copies of pods with modified configuration, or attach a debug " +
			"container to a node.",
		build: func(f cmdutil.Factory, s genericiooptions.IOStreams) *cobra.Command {
			return debug.NewCmdDebug(f, s)
		},
	}, kubeConfigPath)
}

// CreateExecCommand creates a kubectl exec command with all its flags and behavior.
func (c *Client) CreateExecCommand(kubeConfigPath string) *cobra.Command {
	return c.newWrappedCommand(commandSpec{
		use:   "exec",
		short: "Execute a command in a container",
		long:  "Execute a command in a container in a pod.",
		build: exec.NewCmdExec,
	}, kubeConfigPath)
}

// CreatePortForwardCommand creates a kubectl port-forward command with all its flags and behavior.
func (c *Client) CreatePortForwardCommand(kubeConfigPath string) *cobra.Command {
	return c.newWrappedCommand(commandSpec{
		use:   "forward",
		short: "Forward one or more local ports to a pod",
		long: "Forward one or more local ports to a pod.\n\n" +
			"Use resource type/name such as deployment/mydeployment to select a pod. " +
			"Resource type defaults to 'pod' if omitted.\n\n" +
			"If there are multiple pods matching the criteria, a pod will be selected automatically. " +
			"The forwarding session ends when the selected pod terminates, and a rerun of the " +
			"command is needed to resume forwarding.",
		build: portforward.NewCmdPortForward,
		// The upstream examples use "port-forward" which customizeCommand turns into
		// "ksail workload port-forward". Fix to match the renamed "forward" subcommand.
		postCustomize: func(cmd *cobra.Command) {
			cmd.Example = strings.ReplaceAll(cmd.Example, "port-forward", "forward")
		},
	}, kubeConfigPath)
}

// CreateClusterInfoCommand wires kubectl's cluster-info with minimal guarding.
// When contextName is non-empty the command is scoped to that kubeconfig
// context; otherwise kubectl falls back to the kubeconfig's current context.
func (c *Client) CreateClusterInfoCommand(kubeConfigPath, contextName string) *cobra.Command {
	configFlags := genericclioptions.NewConfigFlags(true)
	if kubeConfigPath != "" {
		configFlags.KubeConfig = &kubeConfigPath
	}

	if contextName != "" {
		configFlags.Context = &contextName
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
