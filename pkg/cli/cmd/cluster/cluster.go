package cluster

import (
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster/oidc"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/project"
	projectenv "github.com/devantler-tech/ksail/v7/pkg/cli/cmd/project/env"
	"github.com/spf13/cobra"
)

const (
	dirPerm  = 0o750
	filePerm = 0o600
	// bytesPerMB is the number of bytes in a megabyte.
	bytesPerMB = 1024 * 1024
	// minCompressionLevel is the minimum gzip compression level.
	minCompressionLevel = -1
	// maxCompressionLevel is the maximum gzip compression level.
	maxCompressionLevel = 9
)

// permissionWrite is the annotations.AnnotationPermission value that marks a
// command as state-modifying (and therefore requiring user confirmation).
const permissionWrite = "write"

// ErrKubeconfigNotFound is returned when no kubeconfig path can be resolved.
var ErrKubeconfigNotFound = errors.New(
	"kubeconfig not found; ensure cluster is created and configured",
)

// ErrInvalidCompressionLevel is returned when the compression level is
// outside the valid range.
var ErrInvalidCompressionLevel = errors.New(
	"compression level out of range",
)

// ErrEmptyPinnedVersion is returned when spec.cluster.talos.version is set to
// an empty string in a context where a pinned version is expected.
var ErrEmptyPinnedVersion = errors.New(
	"pinned distribution version is empty",
)

// ErrDriftDetected is returned by the diff command when --exit-code is set
// and configuration drift is detected. The exit code is 2 (KSail-specific:
// 0 = no drift, 1 = error, 2 = drift detected — not the diff(1) convention).
var ErrDriftDetected = errors.New("configuration drift detected")

// DriftExitError is an error type that carries a custom exit code for the diff
// command. It wraps ErrDriftDetected so callers can use errors.Is, and exposes
// KSailExitCode() so main.go can propagate exit code 2 instead of the generic 1.
type DriftExitError struct {
	Changes int
}

// Error implements the error interface.
func (e *DriftExitError) Error() string {
	return fmt.Sprintf("%v: %d change(s) detected", ErrDriftDetected, e.Changes)
}

// Unwrap returns ErrDriftDetected so errors.Is(err, ErrDriftDetected) works.
func (e *DriftExitError) Unwrap() error {
	return ErrDriftDetected
}

// KSailExitCode returns the exit code that main.go should use for this error.
// Using a KSail-specific name avoids matching stdlib *exec.ExitError, which also
// implements ExitCode() int but represents a real subprocess failure.
func (e *DriftExitError) KSailExitCode() int {
	return diffExitCode
}

// NewClusterCmd creates the parent cluster command and wires lifecycle subcommands beneath it.
func NewClusterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage cluster lifecycle",
		Long: `Manage lifecycle operations for local Kubernetes clusters, including ` +
			`provisioning, teardown, and status.`,
		Args:         cobra.NoArgs,
		RunE:         handleClusterRunE,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationConsolidate: "command",
		},
	}

	cmd.AddCommand(newDeprecatedInitCmd())
	cmd.AddCommand(newDeprecatedAddEnvironmentCmd())
	cmd.AddCommand(NewCreateCmd())
	cmd.AddCommand(NewUpdateCmd())
	cmd.AddCommand(NewDeleteCmd())
	cmd.AddCommand(NewStartCmd())
	cmd.AddCommand(NewStopCmd())
	cmd.AddCommand(NewListCmd())
	cmd.AddCommand(NewInfoCmd())
	cmd.AddCommand(NewDiagnoseCmd())
	cmd.AddCommand(NewDiffCmd())
	cmd.AddCommand(NewConnectCmd())
	cmd.AddCommand(NewBackupCmd())
	cmd.AddCommand(NewRestoreCmd())
	cmd.AddCommand(NewSwitchCmd())
	cmd.AddCommand(NewRepairCmd(nil))
	cmd.AddCommand(oidc.NewOIDCCmd())

	return cmd
}

// newDeprecatedInitCmd returns the init command as a hidden, deprecated alias
// under `cluster`. The command moved to `ksail project init` (issue #5626); this
// alias keeps the previously released `ksail cluster init` working for one
// deprecation cycle, printing a notice that points at the new location. It is
// Hidden so it stays out of help, the generated docs, and the MCP/chat tool
// surface (toolgen skips hidden commands) — the canonical `project init` is the
// only surfaced form.
func newDeprecatedInitCmd() *cobra.Command {
	cmd := project.NewInitCmd()
	cmd.Hidden = true
	cmd.Deprecated = `use "ksail project init" instead`

	return cmd
}

// newDeprecatedAddEnvironmentCmd returns the add-environment command as a hidden,
// deprecated alias under `cluster`. The command moved to `ksail project
// add-environment` (issue #5626) and then into the `project env` group as
// `ksail project env add` (issue #6057); this alias keeps the previously
// released `ksail cluster add-environment` working for one deprecation cycle,
// printing a notice that points at the new location. The rebadging is shared
// with the project group's delegate (projectenv.NewDeprecatedAddEnvironmentDelegate).
func newDeprecatedAddEnvironmentCmd() *cobra.Command {
	return projectenv.NewDeprecatedAddEnvironmentDelegate()
}

//nolint:gochecknoglobals // Injected for testability to simulate help failures.
var helpRunner = func(cmd *cobra.Command) error {
	return cmd.Help()
}

func handleClusterRunE(cmd *cobra.Command, _ []string) error {
	// Cobra Help() can return an error (e.g., output stream or template issues); wrap it for clarity.
	err := helpRunner(cmd)
	if err != nil {
		return fmt.Errorf("displaying cluster command help: %w", err)
	}

	return nil
}
