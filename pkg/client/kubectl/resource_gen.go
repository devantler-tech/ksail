package kubectl

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

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

// createFreshClient creates a fresh client from the command's IO streams.
func (c *Client) createFreshClient(cmd *cobra.Command) *Client {
	return NewClient(genericiooptions.IOStreams{
		In:     cmd.InOrStdin(),
		Out:    cmd.OutOrStdout(),
		ErrOut: cmd.ErrOrStderr(),
	})
}

// prepareAndExecuteGen is a helper that handles the common pattern of:
// 1. Finding the target command
// 2. Setting forced flags
// 3. Setting output streams
// 4. Copying user flags
// 5. Executing the command.
func (c *Client) prepareAndExecuteGen(
	targetCmd *cobra.Command,
	wrapperCmd *cobra.Command,
	args []string,
) error {
	// Force --dry-run=client and -o yaml
	err := c.setForcedFlags(targetCmd)
	if err != nil {
		return err
	}

	// Ensure command output is captured by the wrapper command
	targetCmd.SetOut(wrapperCmd.OutOrStdout())
	targetCmd.SetErr(wrapperCmd.ErrOrStderr())

	// Copy user flags
	err = c.copyUserFlags(wrapperCmd, targetCmd)
	if err != nil {
		return err
	}

	// Execute
	return c.executeCommand(targetCmd, args)
}

// executeSubcommandGen executes kubectl create with subcommand and forced flags.
func (c *Client) executeSubcommandGen(
	parentType, subType string,
	cmd *cobra.Command,
	args []string,
) error {
	freshClient := c.createFreshClient(cmd)
	createCmd := freshClient.CreateCreateCommand("")

	// Find the parent resource command
	parentCmd := freshClient.findResourceCommand(createCmd, parentType)
	if parentCmd == nil {
		return fmt.Errorf("%w: %s", ErrResourceCommandNotFound, parentType)
	}

	// Find the subcommand
	freshSubCmd := freshClient.findResourceCommand(parentCmd, subType)
	if freshSubCmd == nil {
		return fmt.Errorf("%w: %s %s", ErrResourceCommandNotFound, parentType, subType)
	}

	return freshClient.prepareAndExecuteGen(freshSubCmd, cmd, args)
}

// executeResourceGen executes kubectl create with forced --dry-run=client -o yaml flags.
func (c *Client) executeResourceGen(resourceType string, cmd *cobra.Command, args []string) error {
	freshClient := c.createFreshClient(cmd)
	createCmd := freshClient.CreateCreateCommand("")

	freshResourceCmd := freshClient.findResourceCommand(createCmd, resourceType)
	if freshResourceCmd == nil {
		return fmt.Errorf("%w: %s", ErrResourceCommandNotFound, resourceType)
	}

	return freshClient.prepareAndExecuteGen(freshResourceCmd, cmd, args)
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

// executeCommand executes the kubectl command's Run or RunE function safely.
// kubectl commands use Run with cmdutil.CheckErr which calls os.Exit on error.
// This method intercepts fatal errors via BehaviorOnFatal + panic/recover.
func (c *Client) executeCommand(cmd *cobra.Command, args []string) error {
	if cmd.RunE != nil {
		err := cmd.RunE(cmd, args)
		if err != nil {
			return fmt.Errorf("kubectl command execution failed: %w", err)
		}

		return nil
	}

	if cmd.Run != nil {
		return executeSafeRun(cmd, args)
	}

	return ErrNoRunFunction
}

// executeSafeRun wraps cmd.Run in a panic/recover to catch os.Exit from
// kubectl's cmdutil.CheckErr. This is necessary because kubectl commands
// use Run (not RunE) with CheckErr which calls os.Exit on any error.
func executeSafeRun(cmd *cobra.Command, args []string) (retErr error) {
	cmdutil.BehaviorOnFatal(func(msg string, code int) {
		panic(&kubectlFatalError{msg: msg, code: code})
	})

	defer func() {
		cmdutil.DefaultBehaviorOnFatal()

		if r := recover(); r != nil {
			if e, ok := r.(*kubectlFatalError); ok {
				retErr = fmt.Errorf("kubectl command execution failed: %w", e)
			} else {
				panic(r)
			}
		}
	}()

	cmd.Run(cmd, args)

	return nil
}
