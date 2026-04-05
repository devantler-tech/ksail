package workload

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

const requiredInstallArgs = 2

// NewInstallCmd creates the workload install command.
func NewInstallCmd(_ *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install [NAME] [CHART]",
		Short: "Install Helm charts",
		Long: "Install Helm charts to provision workloads through KSail. " +
			"This command provides native Helm chart installation capabilities.",
		Args: cobra.ExactArgs(requiredInstallArgs),
		RunE: runInstall,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	flags := cmd.Flags()
	flags.StringP("namespace", "n", "default", "namespace scope for the request")
	flags.String("version", "", "chart version constraint (default: latest)")
	flags.Duration(
		"timeout",
		helm.DefaultTimeout,
		"time to wait for any individual Kubernetes operation",
	)
	flags.Bool("create-namespace", false, "create the release namespace if not present")
	flags.Bool("wait", false, "wait until resources are ready")
	flags.Bool("atomic", false, "if set, the installation deletes on failure")

	return cmd
}

func runInstall(cmd *cobra.Command, args []string) error {
	releaseName := args[0]
	chartName := args[1]

	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(cmd)

	client, err := helm.NewClient(kubeconfigPath, "")
	if err != nil {
		return fmt.Errorf("create helm client: %w", err)
	}

	spec := buildChartSpec(cmd, releaseName, chartName)

	_, err = client.InstallChart(cmd.Context(), spec)
	if err != nil {
		return fmt.Errorf("install chart %q: %w", chartName, err)
	}

	return nil
}

func buildChartSpec(cmd *cobra.Command, releaseName, chartName string) *helm.ChartSpec {
	namespace, _ := cmd.Flags().GetString("namespace")
	if namespace == "" {
		namespace = "default"
	}

	timeout, _ := cmd.Flags().GetDuration("timeout")
	version, _ := cmd.Flags().GetString("version")
	createNamespace, _ := cmd.Flags().GetBool("create-namespace")
	wait, _ := cmd.Flags().GetBool("wait")
	atomic, _ := cmd.Flags().GetBool("atomic")

	return &helm.ChartSpec{
		ReleaseName:     releaseName,
		ChartName:       chartName,
		Namespace:       namespace,
		Timeout:         timeout,
		Version:         version,
		CreateNamespace: createNamespace,
		Wait:            wait,
		Atomic:          atomic,
	}
}
