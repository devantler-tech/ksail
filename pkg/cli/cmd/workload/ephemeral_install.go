package workload

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/client/kustomize"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/fluxsubst"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/render"
	"github.com/spf13/cobra"
)

// newEphemeralHelmClient builds the install-capable Helm client an --ephemeral
// run uses against the throwaway cluster. A package var so tests can
// substitute a fake without a live cluster.
//
//nolint:gochecknoglobals // test seam, mirrors newEphemeralProvisioner in this package
var newEphemeralHelmClient = func(kubeconfigPath, kubeContext string) (helm.Interface, error) {
	return helm.NewClient(kubeconfigPath, kubeContext)
}

// installDeclaredCharts installs every chart the workload's HelmReleases
// declare under sourcePath into the ephemeral cluster, so the declared
// operators are present (their CRDs registered) before the workload's
// manifests are exercised against the cluster (ksail#5919 Phase 3b-2; Phase
// 3b-3 applies the rendered manifests and scans their children).
//
// Failure semantics mirror the offline render pipeline: a HelmRelease whose
// chart source cannot be resolved from the stream degrades with a warning
// (the offline validate/scan still covers it), and a kustomization that fails
// to build is skipped here because the validation step that follows reports
// that same failure with proper attribution. A chart that RESOLVES but fails
// to install is a hard error — the run promised cluster-backed coverage for
// it and silently skipping would be a silent no-op.
//
// Installs do not wait for workload readiness (Wait stays false): the point
// is registering the operators' CRDs and admission surface, and on a KWOK
// backend real controller pods never run to completion anyway.
func installDeclaredCharts(
	ctx context.Context,
	cmd *cobra.Command,
	cluster ephemeralCluster,
	sourcePath string,
) error {
	specs, degradations, err := enumerateDeclaredCharts(ctx, sourcePath)
	if err != nil {
		return err
	}

	warnDegradations(cmd, degradations)

	if len(specs) == 0 {
		return nil
	}

	client, err := newEphemeralHelmClient(cluster.KubeconfigPath, cluster.Context)
	if err != nil {
		return fmt.Errorf("create helm client for ephemeral cluster %q: %w", cluster.Name, err)
	}

	for _, spec := range specs {
		notify.Infof(
			cmd.OutOrStdout(),
			"installing declared chart %q into ephemeral cluster %q...",
			spec.ReleaseName, cluster.Name,
		)

		spec.CreateNamespace = true

		installErr := helm.InstallChartWithRetry(ctx, client, spec, spec.ChartName)
		if installErr != nil {
			return fmt.Errorf(
				"install declared chart %q into ephemeral cluster %q: %w",
				spec.ReleaseName, cluster.Name, installErr,
			)
		}
	}

	return nil
}

// withPreparedEphemeralCluster wraps runFn in a throwaway cluster whose
// declared charts are installed first (resolve source → installDeclaredCharts
// → run), the sequence the validate and scan commands share for their
// --ephemeral mode (ksail#5919 Phase 3b-2).
func withPreparedEphemeralCluster(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	runFn func(ctx context.Context) error,
) error {
	return withEphemeralCluster(
		ctx,
		cmd,
		func(ctx context.Context, cluster ephemeralCluster) error {
			sourcePath, pathErr := resolveEphemeralSourcePath(cmd, args)
			if pathErr != nil {
				return pathErr
			}

			installErr := installDeclaredCharts(ctx, cmd, cluster, sourcePath)
			if installErr != nil {
				return installErr
			}

			return runFn(ctx)
		},
	)
}

// resolveEphemeralSourcePath derives the workload source path an --ephemeral
// run installs declared charts from, mirroring exactly how the inner
// validate/scan run derives its own target: single silent config load →
// args/config/cwd precedence → canonicalized.
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

// enumerateDeclaredCharts builds every kustomization under sourcePath and
// enumerates the install-ready chart specs their HelmReleases declare,
// deduplicated by release namespace/name (several kustomizations under one
// tree can include the same base). Build failures are skipped — the
// validation that follows the install step reports them with attribution.
func enumerateDeclaredCharts(
	ctx context.Context,
	sourcePath string,
) ([]*helm.ChartSpec, []render.Degradation, error) {
	kustomizations, err := findKustomizations(sourcePath)
	if err != nil {
		return nil, nil, fmt.Errorf("find kustomizations: %w", err)
	}

	kustomizeClient := kustomize.NewClient()

	var (
		specs        []*helm.ChartSpec
		degradations []render.Degradation
	)

	seen := make(map[string]bool)

	for _, kustDir := range kustomizations {
		output, buildErr := kustomizeClient.Build(ctx, kustDir)
		if buildErr != nil {
			continue
		}

		stream := fluxsubst.ExpandFluxSubstitutions(output.Bytes())

		dirSpecs, dirDegradations := render.EnumerateChartSpecs(stream)
		degradations = append(degradations, dirDegradations...)

		for _, spec := range dirSpecs {
			key := spec.Namespace + "/" + spec.ReleaseName
			if seen[key] {
				continue
			}

			seen[key] = true

			specs = append(specs, spec)
		}
	}

	return specs, degradations, nil
}
