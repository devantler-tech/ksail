package workload

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/ephemeral"
	"github.com/spf13/cobra"
)

const ephemeralAdmissionTimeout = 10 * time.Minute

const ephemeralFlagDescription = "EXPERIMENTAL: run offline checks, then check workload admission " +
	"in an isolated throwaway Kind cluster with declared Helm charts installed and ready " +
	"(guaranteed teardown; off by default). Operator-generated children are not inspected."

// newEphemeralHelmClient constructs a client pinned to the throwaway cluster.
//
//nolint:gochecknoglobals // lifecycle test seam
var newEphemeralHelmClient = func(kubeconfigPath, kubeContext string) (helm.Interface, error) {
	return helm.NewClient(kubeconfigPath, kubeContext)
}

// newEphemeralAdmissionClient constructs a client pinned to the throwaway cluster.
//
//nolint:gochecknoglobals // lifecycle test seam
var newEphemeralAdmissionClient = func(kubeconfigPath, kubeContext string) (ephemeral.Client, error) {
	return ephemeral.NewApplier(kubeconfigPath, kubeContext)
}

// installEphemeralChart waits for a declared chart's workloads and jobs before admission checks.
func installEphemeralChart(
	ctx context.Context,
	cmd *cobra.Command,
	cluster ephemeralCluster,
	spec *helm.ChartSpec,
) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("ephemeral chart installation cancelled: %w", err)
	}

	client, err := newEphemeralHelmClient(cluster.KubeconfigPath, cluster.Context)
	if err != nil {
		return fmt.Errorf("create helm client for ephemeral cluster %q: %w", cluster.Name, err)
	}

	readySpec := *spec
	readySpec.CreateNamespace = true
	readySpec.Wait = true
	readySpec.WaitForJobs = true

	readySpec.Timeout = helm.DefaultTimeout
	if deadline, ok := ctx.Deadline(); ok {
		readySpec.Timeout = min(readySpec.Timeout, time.Until(deadline))
	}

	notify.Infof(
		cmd.OutOrStdout(),
		"installing and waiting for declared chart %q...",
		spec.ReleaseName,
	)

	err = helm.InstallChartWithRetry(ctx, client, &readySpec, spec.ChartName)
	if err != nil {
		return fmt.Errorf(
			"install declared chart %q into ephemeral cluster %q: %w",
			spec.ReleaseName,
			cluster.Name,
			err,
		)
	}

	return nil
}

// withPreparedEphemeralCluster verifies the selected input and offline gate before
// provisioning. All admission operations share a bounded context, and the lifecycle
// owns teardown independently of admission success or cancellation.
func withPreparedEphemeralCluster(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	runFn func(context.Context) error,
) error {
	sourcePath, err := resolveEphemeralSourcePath(cmd, args)
	if err != nil {
		return err
	}

	plan, err := ephemeral.Load(ctx, sourcePath)
	if err != nil {
		return fmt.Errorf("prepare ephemeral admission: %w", err)
	}

	err = runFn(ctx)
	if err != nil {
		return err
	}

	return withEphemeralCluster(
		ctx,
		cmd,
		func(ctx context.Context, cluster ephemeralCluster) error {
			admissionCtx, cancel := context.WithTimeout(ctx, ephemeralAdmissionTimeout)
			defer cancel()

			var client ephemeral.Client

			if len(plan.Namespaces)+len(plan.CRDs)+len(plan.Configuration)+len(plan.Resources) > 0 {
				var err error

				client, err = newEphemeralAdmissionClient(cluster.KubeconfigPath, cluster.Context)
				if err != nil {
					return fmt.Errorf("create ephemeral admission client: %w", err)
				}
			}

			notify.Infof(
				cmd.OutOrStdout(),
				"checking workload admission in ephemeral cluster %q...",
				cluster.Name,
			)

			err := plan.Run(
				admissionCtx,
				client,
				func(ctx context.Context, spec *helm.ChartSpec) error {
					return installEphemeralChart(ctx, cmd, cluster, spec)
				},
			)
			if err != nil {
				return fmt.Errorf("ephemeral admission failed: %w", err)
			}

			notify.Infof(cmd.OutOrStdout(), "ephemeral admission checks passed")

			return nil
		},
	)
}

// resolveEphemeralSourcePath uses the same argument/config/cwd precedence as offline checks.
func resolveEphemeralSourcePath(cmd *cobra.Command, args []string) (string, error) {
	cfg, configFound, loadErr := loadValidateConfigSilently(cmd)

	path, err := resolveValidatePath(args, cfg, configFound, loadErr)
	if err != nil {
		return "", err
	}

	canonPath, err := fsutil.EvalCanonicalPath(path)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}

	return canonPath, nil
}
