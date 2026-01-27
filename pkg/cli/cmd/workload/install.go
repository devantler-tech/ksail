package workload

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

const requiredInstallArgs = 2

// NewInstallCmd creates the workload install command.
func NewInstallCmd(_ *runtime.Runtime) *cobra.Command {
	// Try to load config silently to get kubeconfig path
	kubeconfigPath := helpers.GetKubeconfigPathSilently()

	cmd := &cobra.Command{
		Use:   "install [NAME] [CHART]",
		Short: "Install Helm charts",
		Long: "Install Helm charts to provision workloads through KSail. " +
			"This command provides native Helm chart installation capabilities.",
		Args: cobra.ExactArgs(requiredInstallArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			releaseName := args[0]
			chartName := args[1]

			// Create helm client
			client, err := helm.NewClient(kubeconfigPath, "")
			if err != nil {
				return fmt.Errorf("create helm client: %w", err)
			}

			// Get namespace from flag or use default
			namespace, _ := cmd.Flags().GetString("namespace")
			if namespace == "" {
				namespace = "default"
			}

			// Create chart spec
			spec := &helm.ChartSpec{
				ReleaseName: releaseName,
				ChartName:   chartName,
				Namespace:   namespace,
				Timeout:     helm.DefaultTimeout,
			}

			// Get other flags
			if createNamespace, _ := cmd.Flags().GetBool("create-namespace"); createNamespace {
				spec.CreateNamespace = true
			}

			if wait, _ := cmd.Flags().GetBool("wait"); wait {
				spec.Wait = true
			}

			if atomic, _ := cmd.Flags().GetBool("atomic"); atomic {
				spec.Atomic = true
			}

			// Install chart
			_, err = client.InstallChart(cmd.Context(), spec)
			if err != nil {
				return fmt.Errorf("install chart %q: %w", chartName, err)
			}

			return nil
		},
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	// Add basic Helm install flags
	flags := cmd.Flags()
	flags.StringP("namespace", "n", "default", "namespace scope for the request")
	flags.Bool("create-namespace", false, "create the release namespace if not present")
	flags.Bool("wait", false, "wait until resources are ready")
	flags.Bool("atomic", false, "if set, the installation deletes on failure")

	return cmd
}
