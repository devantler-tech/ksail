package gen

import (
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/spf13/cobra"
)

// NewClusterRoleCmd creates the gen clusterrole command.
func NewClusterRoleCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreateClusterRoleCmd)
}

// NewClusterRoleBindingCmd creates the gen clusterrolebinding command.
func NewClusterRoleBindingCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreateClusterRoleBindingCmd)
}

// NewConfigMapCmd creates the gen configmap command.
func NewConfigMapCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreateConfigMapCmd)
}

// NewCronJobCmd creates the gen cronjob command.
func NewCronJobCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreateCronJobCmd)
}

// NewDeploymentCmd creates the gen deployment command.
func NewDeploymentCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreateDeploymentCmd)
}

// createGenCmd is a helper that creates a gen command by calling the provided kubectl method.
func createGenCmd(
	createMethod func(*kubectl.Client) (*cobra.Command, error),
) *cobra.Command {
	client := kubectl.NewClientWithStdio()
	cmd, err := createMethod(client)
	cobra.CheckErr(err)

	return cmd
}

// NewGenCmd creates and returns the gen command group namespace.
func NewGenCmd() *cobra.Command {
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

	cmd.AddCommand(NewClusterRoleCmd())
	cmd.AddCommand(NewClusterRoleBindingCmd())
	cmd.AddCommand(NewConfigMapCmd())
	cmd.AddCommand(NewCronJobCmd())
	cmd.AddCommand(NewDeploymentCmd())
	cmd.AddCommand(NewHelmReleaseCmd())
	cmd.AddCommand(NewIngressCmd())
	cmd.AddCommand(NewJobCmd())
	cmd.AddCommand(NewNamespaceCmd())
	cmd.AddCommand(NewPodDisruptionBudgetCmd())
	cmd.AddCommand(NewPriorityClassCmd())
	cmd.AddCommand(NewQuotaCmd())
	cmd.AddCommand(NewRoleCmd())
	cmd.AddCommand(NewRoleBindingCmd())
	cmd.AddCommand(NewSecretCmd())
	cmd.AddCommand(NewServiceCmd())
	cmd.AddCommand(NewServiceAccountCmd())

	return cmd
}

// NewIngressCmd creates the gen ingress command.
func NewIngressCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreateIngressCmd)
}

// NewJobCmd creates the gen job command.
func NewJobCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreateJobCmd)
}

// NewNamespaceCmd creates the gen namespace command.
func NewNamespaceCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreateNamespaceCmd)
}

// NewPodDisruptionBudgetCmd creates the gen poddisruptionbudget command.
func NewPodDisruptionBudgetCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreatePodDisruptionBudgetCmd)
}

// NewPriorityClassCmd creates the gen priorityclass command.
func NewPriorityClassCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreatePriorityClassCmd)
}

// NewQuotaCmd creates the gen quota command.
func NewQuotaCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreateQuotaCmd)
}

// NewRoleCmd creates the gen role command.
func NewRoleCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreateRoleCmd)
}

// NewRoleBindingCmd creates the gen rolebinding command.
func NewRoleBindingCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreateRoleBindingCmd)
}

// NewSecretCmd creates the gen secret command.
func NewSecretCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreateSecretCmd)
}

// NewServiceCmd creates the gen service command.
func NewServiceCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreateServiceCmd)
}

// NewServiceAccountCmd creates the gen serviceaccount command.
func NewServiceAccountCmd() *cobra.Command {
	return createGenCmd((*kubectl.Client).CreateServiceAccountCmd)
}
