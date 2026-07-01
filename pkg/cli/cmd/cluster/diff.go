package cluster

import (
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/clusterflags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
)

// diffLongDesc describes the `ksail cluster diff` command.
const diffLongDesc = `Compare the desired cluster configuration (ksail.yaml) against the live
cluster state and report any drift.

This command detects installed components via the Kubernetes API and Helm
release history, then compares them against the configuration defined in
ksail.yaml. Changes are classified by impact (in-place, reboot-required,
recreate-required, wipe-required) — the same categories used by
'ksail cluster update'.

No changes are applied to the cluster; this is a read-only operation.

The cluster is resolved in the following priority order:
  1. From the --name flag override
  2. From metadata.name in the ksail.yaml config file
  3. From the current kubeconfig context

Use --output json for machine-readable output suitable for CI pipelines.
Use --exit-code to return exit code 2 when drift is detected (useful for
CI gates and monitoring scripts).

Use --include-version-drift to also report the distribution/Kubernetes version
reconciliation 'ksail cluster update' applies on every run (matching
'update --dry-run'). It is off by default: resolving the latest available
version performs OCI registry lookups, so enabling it makes diff
network-dependent and may report drift whenever upstream cuts a release.`

// NewDiffCmd creates the cluster diff command.
func NewDiffCmd() *cobra.Command {
	var (
		exitCodeFlag        bool
		includeVersionDrift bool
	)

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show configuration drift between ksail.yaml and live cluster",
		Long:  diffLongDesc,
		Annotations: map[string]string{
			annotations.AnnotationDescription: "Compare desired cluster configuration against live state and report drift",
		},
		SilenceUsage: true,
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	// Hide flags that diff doesn't expose in its help but that are needed for
	// config defaults and validation to work correctly.
	hideConfigOnlyFlags(cmd)

	cmd.Flags().String("output", "text",
		"Output format: text or json. Use json for machine-readable structured output.")

	cmd.Flags().BoolVar(&exitCodeFlag, "exit-code", false,
		"Return exit code 2 when drift is detected (for CI gates)")

	cmd.Flags().BoolVar(&includeVersionDrift, "include-version-drift", false,
		"Also report distribution/Kubernetes version drift the next 'cluster update' "+
			"would reconcile (performs OCI registry lookups; off by default)")

	clusterflags.RegisterNameFlag(cmd, cfgManager)

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		// Validate output format before entering WrapHandler to avoid unnecessary
		// DI when the format is obviously invalid. Mirrors the diagnose pattern.
		err := validateOutputFormat(cmd)
		if err != nil {
			return err
		}

		format := getOutputFormat(cmd)

		handler := lifecycle.WrapHandler(
			cfgManager,
			func(cmd *cobra.Command, cfgManager *ksailconfigmanager.ConfigManager, deps lifecycle.Deps) error {
				return handleDiffRunE(cmd, cfgManager, deps, diffOptions{
					exitCode:            exitCodeFlag,
					format:              format,
					includeVersionDrift: includeVersionDrift,
				})
			},
		)

		return handler(cmd, nil)
	}

	return cmd
}

// diffOptions bundles the per-invocation diff flags so handleDiffRunE keeps a
// short signature as options accrue.
type diffOptions struct {
	exitCode            bool
	format              string
	includeVersionDrift bool
}

// handleDiffRunE computes the diff between desired and live cluster state.
func handleDiffRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
	opts diffOptions,
) error {
	ctx, _, err := loadAndValidateClusterConfig(cfgManager, deps)
	if err != nil {
		return err
	}

	diff := computeSpecOnlyDiff(cmd, ctx)

	// Also try provisioner-level diff for Updater-capable provisioners.
	// This adds distribution-specific changes (e.g., node counts, Talos config).
	mergeProvisionerDiff(cmd, ctx, diff)

	// Opt-in: also report version-reconciliation drift (matches update --dry-run).
	if opts.includeVersionDrift {
		mergeVersionDrift(cmd, ctx, diff)
	}

	if diff.TotalChanges() == 0 && !diff.HasUnknownBaseline() {
		if opts.format == outputFormatJSON {
			emitDiffJSON(cmd, diff)
		} else {
			notify.Infof(cmd.OutOrStdout(), "No configuration drift detected")
		}

		return nil
	}

	displayDiffResult(cmd, diff, opts.format)

	if opts.exitCode {
		// An unknown baseline is treated as drift: the tool cannot confirm the
		// cluster matches the desired configuration.
		return &DriftExitError{Changes: diff.TotalChanges() + len(diff.UnknownBaseline)}
	}

	return nil
}

// displayDiffResult renders the diff output in the requested format.
func displayDiffResult(
	cmd *cobra.Command,
	diff *clusterupdate.UpdateResult,
	format string,
) {
	if format == outputFormatJSON {
		emitDiffJSON(cmd, diff)

		return
	}

	notify.Titlef(cmd.OutOrStdout(), "🔍", "Configuration drift")

	notify.Infof(
		cmd.OutOrStdout(),
		formatDiffTable(diff),
	)
}

// mergeProvisionerDiff attempts to compute and merge provisioner-specific diffs
// for distributions that support the Updater interface. Errors are logged as
// warnings and do not block the main spec-level diff.
func mergeProvisionerDiff(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	mainDiff *clusterupdate.UpdateResult,
) {
	factory := newProvisionerFactory(ctx)

	provisioner, _, err := factory.Create(cmd.Context(), ctx.ClusterCfg)
	if err != nil {
		notify.Warningf(
			cmd.ErrOrStderr(),
			"Cannot create provisioner for provisioner-level diff (drift detection may be incomplete): %v",
			err,
		)

		return
	}

	updater, ok := provisioner.(clusterprovisioner.Updater)
	if !ok {
		return
	}

	clusterName := resolveClusterNameFromContext(ctx)

	currentSpec, _, err := updater.GetCurrentConfig(cmd.Context(), clusterName)
	if err != nil {
		notify.Warningf(cmd.ErrOrStderr(),
			"Cannot retrieve current config for provisioner-level diff: %v", err)

		return
	}

	provisionerDiff, err := updater.DiffConfig(
		cmd.Context(), clusterName, currentSpec, &ctx.ClusterCfg.Spec.Cluster,
	)
	if err != nil {
		notify.Warningf(cmd.ErrOrStderr(),
			"Cannot compute provisioner-level diff: %v", err)

		return
	}

	specdiff.MergeProvisionerDiff(mainDiff, provisionerDiff)
}
