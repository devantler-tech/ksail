package cluster

import (
	"context"
	"errors"

	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/repairer"
	talosconfigrepair "github.com/devantler-tech/ksail/v7/pkg/svc/repairer/talosconfig"
	"github.com/spf13/cobra"
)

// NewRepairCmd creates the `ksail cluster repair` command, backed by
// the supplied [repairer.Registry]. Pass [repairer.Default] for normal
// operation; tests can pass an isolated registry from
// [repairer.NewRegistry] to avoid cross-package contention.
//
// The command runs every [repairer.Repair] registered with the
// supplied registry, printing one status line per repair. It is
// idempotent and safe to run repeatedly. The first registered repair
// fixes a known single-byte corruption in Talos talosconfig CA bytes
// that produces:
//
//	failed to append CA certificate to RootCAs pool
//
// during `ksail cluster update`.
func NewRepairCmd(_ *di.Runtime, registry *repairer.Registry) *cobra.Command {
	if registry == nil {
		registry = repairer.Default()
	}

	var talosconfigPath string

	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Repair local KSail/Talos state files",
		Long: `Detect and repair known corruption patterns in local state files.

Currently supported repairs:
  - talosconfig-ca: fixes a single-byte BasicConstraints corruption in
    the Talos talosconfig CA that prevents 'cluster update' from
    establishing a Talos client.

Each repair is idempotent and writes a timestamped backup of any file
it modifies.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRepair(cmd.Context(), cmd, registry, talosconfigPath)
		},
	}

	cmd.Flags().StringVar(
		&talosconfigPath,
		"talosconfig",
		"",
		"path to talosconfig (default: ~/.talos/config)",
	)

	return cmd
}

func runRepair(
	ctx context.Context,
	cmd *cobra.Command,
	registry *repairer.Registry,
	talosconfigPath string,
) error {
	out := cmd.OutOrStdout()

	repairs := registry.All()
	configurePerRepairOptions(repairs, talosconfigPath)

	if len(repairs) == 0 {
		notify.Activityf(out, "no repairs registered")

		return nil
	}

	var hadError bool

	for _, r := range repairs {
		notify.Activityf(out, "running repair %q...", r.Name())

		result := r.Run(ctx, out)
		printRepairResult(cmd, result)

		if result.Err != nil || result.Status == repairer.StatusUnrepairable {
			hadError = true
		}
	}

	if hadError {
		return errRepairsFailed
	}

	return nil
}

// errRepairsFailed signals that at least one repair returned an error
// or [repairer.StatusUnrepairable]. Cobra picks this up via RunE and
// surfaces it as a non-zero exit.
var errRepairsFailed = errors.New("one or more repairs reported failures")

// configurePerRepairOptions threads CLI flags into individual repair
// implementations that need them. Today only the talosconfig repair
// reads --talosconfig.
func configurePerRepairOptions(repairs []repairer.Repair, talosconfigPath string) {
	if talosconfigPath == "" {
		return
	}

	for _, r := range repairs {
		if tc, ok := r.(*talosconfigrepair.Repair); ok {
			tc.Path = talosconfigPath
		}
	}
}

func printRepairResult(cmd *cobra.Command, result repairer.Result) {
	out := cmd.OutOrStdout()

	switch result.Status {
	case repairer.StatusOK:
		notify.Successf(out, "[%s] %s", result.Name, result.Detail)
	case repairer.StatusRepaired:
		notify.Successf(out, "[%s] %s (backup: %s)", result.Name, result.Detail, result.BackupPath)
	case repairer.StatusUnrepairable:
		notify.Warningf(out, "[%s] %s", result.Name, result.Detail)
	case repairer.StatusSkipped:
		notify.Activityf(out, "[%s] %s", result.Name, result.Detail)
	default:
		notify.Activityf(out, "[%s] %s (status=%s)", result.Name, result.Detail, result.Status)
	}

	if result.Err != nil {
		notify.Errorf(out, "[%s] error: %v", result.Name, result.Err)
	}
}
