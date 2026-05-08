package repairer

import (
	"context"
	"io"
	"sync"
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

// Registry holds a collection of [Repair] implementations. It is safe
// for concurrent use. Tests SHOULD construct an isolated [Registry]
// via [NewRegistry] rather than mutating [Default] to avoid races
// between packages that run in parallel under `go test`.
type Registry struct {
	mu      sync.RWMutex
	repairs []Repair
}

// NewRegistry returns an empty, isolated [Registry] suitable for tests.
func NewRegistry() *Registry { return &Registry{} }

// Register adds r to this registry. If a repair with the same
// [Repair.Name] is already present, Register is a no-op so callers may
// invoke it idempotently (e.g., on every command-tree construction).
func (reg *Registry) Register(repair Repair) {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	name := repair.Name()
	for _, existing := range reg.repairs {
		if existing.Name() == name {
			return
		}
	}

	reg.repairs = append(reg.repairs, repair)
}

// All returns a snapshot of every registered [Repair] in registration
// order.
func (reg *Registry) All() []Repair {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	out := make([]Repair, len(reg.repairs))
	copy(out, reg.repairs)

	return out
}

// defaultRegistry is the process-wide registry that init() functions in
// concrete repair packages populate. Production code uses [Default];
// tests SHOULD prefer [NewRegistry] for isolation.
var defaultRegistry = &Registry{}

// Default returns the process-wide [Registry] populated by repair
// packages via [Registry.Register].
func Default() *Registry { return defaultRegistry }
