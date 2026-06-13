// Package nested holds the shared nested-on-Kubernetes lifecycle skeleton used by
// the *Kubernetes provider* provisioner wrappers (Kind, KWOK, K3d, VCluster, and
// Talos), which run a child cluster inside a host Kubernetes cluster.
//
// Before this package each wrapper hand-rolled the same DinD delete/exists/list
// flow, the same kubeconfig-Secret poll, the same opt-in failure diagnostics, the
// same readiness-timeout env-var override, and the same constants — duplication
// that was silenced with jscpd:ignore markers rather than shared. The helpers
// here are the single source so a new nested distribution composes them instead
// of cloning a sixth copy.
//
// The package is internal to pkg/svc/provisioner/cluster so it cannot leak into
// unrelated callers.
package nested

import (
	"os"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/envvar"
)

const (
	// DebugEnvVar gates opt-in nested-cluster diagnostics. It matches the value the
	// CI nested-provider test sets and the Talos provisioner checks; when unset the
	// diagnostics helpers are no-ops.
	DebugEnvVar = "KSAIL_NESTED_DEBUG"

	// ReadyTimeoutEnvVar overrides a nested provisioner's default readiness wait with
	// a Go duration (e.g. "15m"). It matches the value the CI nested-provider action
	// exports; it lets a slow-but-healthy nested cluster under runner contention avoid
	// a premature context-deadline failure without changing the compiled-in default.
	ReadyTimeoutEnvVar = "KSAIL_NESTED_READY_TIMEOUT"
)

// DebugEnabled reports whether opt-in nested diagnostics are enabled
// (KSAIL_NESTED_DEBUG is set to a non-empty value).
func DebugEnabled() bool {
	return os.Getenv(DebugEnvVar) != ""
}

// ReadyTimeout returns the readiness wait budget for a nested cluster, honoring
// the KSAIL_NESTED_READY_TIMEOUT override and falling back to fallback when the
// env var is unset or unparseable.
func ReadyTimeout(fallback time.Duration) time.Duration {
	return envvar.Duration(ReadyTimeoutEnvVar, fallback)
}
