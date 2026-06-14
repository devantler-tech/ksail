package cluster

import (
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/devantler-tech/ksail/v7/pkg/svc/repairer"
	talosconfigrepair "github.com/devantler-tech/ksail/v7/pkg/svc/repairer/talosconfig"
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
func NewClusterCmd(runtimeContainer *di.Runtime) *cobra.Command {
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

	cmd.AddCommand(NewInitCmd(runtimeContainer))
	cmd.AddCommand(NewCreateCmd(runtimeContainer))
	cmd.AddCommand(NewUpdateCmd(runtimeContainer))
	cmd.AddCommand(NewDeleteCmd(runtimeContainer))
	cmd.AddCommand(NewStartCmd(runtimeContainer))
	cmd.AddCommand(NewStopCmd(runtimeContainer))
	cmd.AddCommand(NewListCmd(runtimeContainer))
	cmd.AddCommand(NewInfoCmd(runtimeContainer))
	cmd.AddCommand(NewDiagnoseCmd(runtimeContainer))
	cmd.AddCommand(NewDiffCmd(runtimeContainer))
	cmd.AddCommand(NewConnectCmd(runtimeContainer))
	cmd.AddCommand(NewBackupCmd(runtimeContainer))
	cmd.AddCommand(NewRestoreCmd(runtimeContainer))
	cmd.AddCommand(NewSwitchCmd(runtimeContainer))
	talosconfigrepair.RegisterDefault(repairer.Default())
	cmd.AddCommand(NewRepairCmd(runtimeContainer, repairer.Default()))

	return cmd
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
