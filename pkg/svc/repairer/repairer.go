package repairer

import (
	"context"
	"io"
)

// Status describes the outcome of a single [Repair.Run] invocation.
type Status int

const (
	// StatusOK means the target is already in a valid state and nothing
	// was modified.
	StatusOK Status = iota
	// StatusRepaired means the repair successfully modified the target.
	StatusRepaired
	// StatusUnrepairable means the target is broken in a way the repair
	// recognises but cannot fix.
	StatusUnrepairable
	// StatusSkipped means the repair did not apply (e.g., target file is
	// missing and the repair only handles existing files).
	StatusSkipped
)

// String returns a stable lowercase label for the status, used by the
// CLI to render per-repair outcomes.
func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusRepaired:
		return "repaired"
	case StatusUnrepairable:
		return "unrepairable"
	case StatusSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// Result describes the outcome of running one [Repair].
type Result struct {
	// Name is the [Repair.Name] of the repair that produced this result.
	Name string
	// Status is the outcome.
	Status Status
	// Detail is a single human-readable line describing the outcome
	// (e.g., the path that was repaired, or the reason it was skipped).
	Detail string
	// BackupPath is the path of the backup file that was created when
	// Status is [StatusRepaired]. Empty otherwise.
	BackupPath string
	// Err is set when the repair encountered a real error (distinct from
	// [StatusUnrepairable], which is an expected outcome).
	Err error
}

// Repair runs a single, well-defined repair operation.
type Repair interface {
	// Name returns a stable, kebab-case identifier for the repair
	// (e.g., "talosconfig-ca").
	Name() string
	// Run performs the repair. Implementations MUST be idempotent and
	// SHOULD print human-readable progress to logWriter.
	Run(ctx context.Context, logWriter io.Writer) Result
}
