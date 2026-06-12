package gen

import (
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/spf13/cobra"
)

// NewClusterRoleCmd creates the gen clusterrole command.
func NewClusterRoleCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateClusterRoleCmd)
}

// NewClusterRoleBindingCmd creates the gen clusterrolebinding command.
func NewClusterRoleBindingCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateClusterRoleBindingCmd)
}

// NewConfigMapCmd creates the gen configmap command.
func NewConfigMapCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateConfigMapCmd)
}

// NewCronJobCmd creates the gen cronjob command.
func NewCronJobCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateCronJobCmd)
}

// NewDeploymentCmd creates the gen deployment command.
func NewDeploymentCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateDeploymentCmd)
}

// createGenCmd is a helper that creates a gen command by calling the provided kubectl method.
func createGenCmd(
	_ *di.Runtime,
	createMethod func(*kubectl.Client) (*cobra.Command, error),
) *cobra.Command {
	client := kubectl.NewClientWithStdio()
	cmd, err := createMethod(client)
	cobra.CheckErr(err)

	return cmd
}

// NewGenCmd creates and returns the gen command group namespace.
func NewGenCmd(runtimeContainer *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate Kubernetes resource manifests",
		Long: "Generate Kubernetes resource manifests using kubectl create with --dry-run=client -o yaml. " +
			"The generated YAML is printed to stdout and can be redirected to a file using shell redirection (> file.yaml).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		SilenceUsage: true,
	}

	cmd.AddCommand(NewClusterRoleCmd(runtimeContainer))
	cmd.AddCommand(NewClusterRoleBindingCmd(runtimeContainer))
	cmd.AddCommand(NewConfigMapCmd(runtimeContainer))
	cmd.AddCommand(NewCronJobCmd(runtimeContainer))
	cmd.AddCommand(NewDeploymentCmd(runtimeContainer))
	cmd.AddCommand(NewHelmReleaseCmd(runtimeContainer))
	cmd.AddCommand(NewIngressCmd(runtimeContainer))
	cmd.AddCommand(NewJobCmd(runtimeContainer))
	cmd.AddCommand(NewNamespaceCmd(runtimeContainer))
	cmd.AddCommand(NewPodDisruptionBudgetCmd(runtimeContainer))
	cmd.AddCommand(NewPriorityClassCmd(runtimeContainer))
	cmd.AddCommand(NewQuotaCmd(runtimeContainer))
	cmd.AddCommand(NewRoleCmd(runtimeContainer))
	cmd.AddCommand(NewRoleBindingCmd(runtimeContainer))
	cmd.AddCommand(NewSecretCmd(runtimeContainer))
	cmd.AddCommand(NewServiceCmd(runtimeContainer))
	cmd.AddCommand(NewServiceAccountCmd(runtimeContainer))

	return cmd
}

// NewIngressCmd creates the gen ingress command.
func NewIngressCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateIngressCmd)
}

// NewJobCmd creates the gen job command.
func NewJobCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateJobCmd)
}

// NewNamespaceCmd creates the gen namespace command.
func NewNamespaceCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateNamespaceCmd)
}

// NewPodDisruptionBudgetCmd creates the gen poddisruptionbudget command.
func NewPodDisruptionBudgetCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreatePodDisruptionBudgetCmd)
}

// NewPriorityClassCmd creates the gen priorityclass command.
func NewPriorityClassCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreatePriorityClassCmd)
}

// NewQuotaCmd creates the gen quota command.
func NewQuotaCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateQuotaCmd)
}

// NewRoleCmd creates the gen role command.
func NewRoleCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateRoleCmd)
}

// NewRoleBindingCmd creates the gen rolebinding command.
func NewRoleBindingCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateRoleBindingCmd)
}

// NewSecretCmd creates the gen secret command.
func NewSecretCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateSecretCmd)
}

// NewServiceCmd creates the gen service command.
func NewServiceCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateServiceCmd)
}

// NewServiceAccountCmd creates the gen serviceaccount command.
func NewServiceAccountCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateServiceAccountCmd)
}
