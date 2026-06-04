package cluster

import (
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
)

// NewDiffCmd creates the cluster diff command.
func NewDiffCmd(runtimeContainer *di.Runtime) *cobra.Command {
	var exitCodeFlag bool

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
	for _, flagName := range []string{"distribution", "distribution-config", "gitops-engine", "local-registry"} {
		if f := cmd.Flags().Lookup(flagName); f != nil {
			f.Hidden = true
		}
	}

	cmd.Flags().String("output", "text",
		"Output format: text or json. Use json for machine-readable structured output.")

	cmd.Flags().BoolVar(&exitCodeFlag, "exit-code", false,
		"Return exit code 2 when drift is detected (for CI gates)")

	registerNameFlag(cmd, cfgManager)

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		// Validate output format before entering WrapHandler to avoid unnecessary
		// DI when the format is obviously invalid. Mirrors the diagnose pattern.
		err := validateOutputFormat(cmd)
		if err != nil {
			return err
		}

		format := getOutputFormat(cmd)

		handler := lifecycle.WrapHandler(
			runtimeContainer,
			cfgManager,
			func(cmd *cobra.Command, cfgManager *ksailconfigmanager.ConfigManager, deps lifecycle.Deps) error {
				return handleDiffRunE(cmd, cfgManager, deps, exitCodeFlag, format)
			},
		)

		return handler(cmd, nil)
	}

	return cmd
}

// handleDiffRunE computes the diff between desired and live cluster state.
func handleDiffRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
	exitCodeFlag bool,
	format string,
) error {
	ctx, _, err := loadAndValidateClusterConfig(cfgManager, deps)
	if err != nil {
		return err
	}

	diff := computeSpecOnlyDiff(cmd, ctx)

	// Also try provisioner-level diff for Updater-capable provisioners.
	// This adds distribution-specific changes (e.g., node counts, Talos config).
	mergeProvisionerDiff(cmd, ctx, diff)

	if diff.TotalChanges() == 0 && !diff.HasUnknownBaseline() {
		if format == outputFormatJSON {
			emitDiffJSON(cmd, diff)
		} else {
			notify.Infof(cmd.OutOrStdout(), "No configuration drift detected")
		}

		return nil
	}

	displayDiffResult(cmd, diff, format)

	if exitCodeFlag {
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
	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			Kind:     ctx.KindConfig,
			K3d:      ctx.K3dConfig,
			Talos:    ctx.TalosConfig,
			VCluster: ctx.VClusterConfig,
			KWOK:     ctx.KWOKConfig,
			EKS:      ctx.EKSConfig,
		},
	}

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
