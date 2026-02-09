// Package clusterupdate provides shared types for cluster update operations.
// These are separated to avoid import cycles between provisioner implementations
// and the main provisioner interface package.
package clusterupdate

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
)

// DefaultLocalRegistryAddress is the default local registry address applied by
// the config system when a GitOps engine is configured but no explicit
// --local-registry flag was provided. Provisioners mirror this default in
// GetCurrentConfig to prevent false-positive diffs.
const DefaultLocalRegistryAddress = "localhost:5050"

// ApplyGitOpsLocalRegistryDefault mirrors the config system's GitOps-aware
// default: when a GitOps engine is detected but no local registry is configured,
// default to "localhost:5050". This prevents false-positive diffs when the update
// command compares the detected current state against the desired state.
func ApplyGitOpsLocalRegistryDefault(spec *v1alpha1.ClusterSpec) {
	if spec.LocalRegistry.Registry != "" {
		return
	}

	switch spec.GitOpsEngine {
	case v1alpha1.GitOpsEngineFlux, v1alpha1.GitOpsEngineArgoCD:
		spec.LocalRegistry.Registry = DefaultLocalRegistryAddress
	case v1alpha1.GitOpsEngineNone, "":
		// No GitOps engine — no default registry needed.
	}
}

// DefaultCurrentSpec returns a ClusterSpec populated with the default values
// that the config system applies at creation time. Provisioners that cannot
// introspect their running config (Kind, K3d) return this to ensure
// DiffEngine does not report false-positive changes.
func DefaultCurrentSpec(
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
) *v1alpha1.ClusterSpec {
	return &v1alpha1.ClusterSpec{
		Distribution:  distribution,
		Provider:      provider,
		CNI:           v1alpha1.CNIDefault,
		CSI:           v1alpha1.CSIDefault,
		MetricsServer: v1alpha1.MetricsServerDefault,
		LoadBalancer:  v1alpha1.LoadBalancerDefault,
		CertManager:   v1alpha1.CertManagerDisabled,
		PolicyEngine:  v1alpha1.PolicyEngineNone,
		GitOpsEngine:  v1alpha1.GitOpsEngineNone,
	}
}

// ChangeCategory classifies the impact of a configuration change.
type ChangeCategory int

const (
	// ChangeCategoryInPlace indicates the change can be applied without disruption.
	// Examples: component enable/disable via Helm, Talos config changes that support NO_REBOOT.
	ChangeCategoryInPlace ChangeCategory = iota

	// ChangeCategoryRebootRequired indicates the change requires node reboots.
	// Examples: Talos kernel parameters, disk encryption settings.
	ChangeCategoryRebootRequired

	// ChangeCategoryRecreateRequired indicates the cluster must be recreated.
	// Examples: distribution change, provider change, Kind node changes, network CIDR changes.
	ChangeCategoryRecreateRequired
)

// String returns a human-readable name for the change category.
func (c ChangeCategory) String() string {
	switch c {
	case ChangeCategoryInPlace:
		return "in-place"
	case ChangeCategoryRebootRequired:
		return "reboot-required"
	case ChangeCategoryRecreateRequired:
		return "recreate-required"
	default:
		return "unknown"
	}
}

// Change describes a single detected configuration change.
type Change struct {
	// Field is the configuration field path that changed (e.g., "cluster.cni", "talos.workers").
	Field string
	// OldValue is the previous value (may be empty for additions).
	OldValue string
	// NewValue is the new value (may be empty for removals).
	NewValue string
	// Category classifies the impact of this change.
	Category ChangeCategory
	// Reason explains why this change has its category.
	Reason string
}

// UpdateResult describes the outcome of a cluster update operation.
type UpdateResult struct {
	// InPlaceChanges lists changes that were applied without disruption.
	InPlaceChanges []Change
	// RebootRequired lists changes that require node reboots.
	RebootRequired []Change
	// RecreateRequired lists changes that require cluster recreation.
	RecreateRequired []Change
	// AppliedChanges lists changes that were successfully applied.
	AppliedChanges []Change
	// FailedChanges lists changes that failed to apply.
	FailedChanges []Change
	// RebootsPerformed indicates how many nodes were rebooted.
	RebootsPerformed int
	// ClusterRecreated indicates if the cluster was recreated.
	ClusterRecreated bool
}

// NewEmptyUpdateResult returns a new UpdateResult with all slices initialized.
// Use this factory instead of manually constructing UpdateResult to avoid
// code duplication across provisioner implementations.
func NewEmptyUpdateResult() *UpdateResult {
	return &UpdateResult{
		InPlaceChanges:   make([]Change, 0),
		RebootRequired:   make([]Change, 0),
		RecreateRequired: make([]Change, 0),
		AppliedChanges:   make([]Change, 0),
		FailedChanges:    make([]Change, 0),
	}
}

// NewDiffResult creates an initialized UpdateResult for DiffConfig methods and
// reports whether both specs are non-nil. Callers should return early with the
// result when ok is false.
func NewDiffResult(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
) (*UpdateResult, bool) {
	return NewEmptyUpdateResult(), oldSpec != nil && newSpec != nil
}

// NewUpdateResultFromDiff creates an UpdateResult seeded with diff classification data
// and initialized applied/failed slices for tracking execution outcomes.
func NewUpdateResultFromDiff(diff *UpdateResult) *UpdateResult {
	return &UpdateResult{
		InPlaceChanges:   diff.InPlaceChanges,
		RebootRequired:   diff.RebootRequired,
		RecreateRequired: diff.RecreateRequired,
		AppliedChanges:   make([]Change, 0),
		FailedChanges:    make([]Change, 0),
	}
}

// HasInPlaceChanges returns true if there are any in-place changes.
func (r *UpdateResult) HasInPlaceChanges() bool {
	return len(r.InPlaceChanges) > 0
}

// HasRebootRequired returns true if there are changes requiring reboots.
func (r *UpdateResult) HasRebootRequired() bool {
	return len(r.RebootRequired) > 0
}

// HasRecreateRequired returns true if there are changes requiring recreation.
func (r *UpdateResult) HasRecreateRequired() bool {
	return len(r.RecreateRequired) > 0
}

// NeedsUserConfirmation returns true if any changes require user confirmation.
// In-place changes can be applied silently; reboot or recreate require confirmation.
func (r *UpdateResult) NeedsUserConfirmation() bool {
	return r.HasRebootRequired() || r.HasRecreateRequired()
}

// TotalChanges returns the total number of detected changes.
func (r *UpdateResult) TotalChanges() int {
	return len(r.InPlaceChanges) + len(r.RebootRequired) + len(r.RecreateRequired)
}

// AllChanges returns all detected changes in a single slice.
func (r *UpdateResult) AllChanges() []Change {
	all := make([]Change, 0, r.TotalChanges())
	all = append(all, r.InPlaceChanges...)
	all = append(all, r.RebootRequired...)
	all = append(all, r.RecreateRequired...)

	return all
}

// UpdateOptions provides configuration for the update operation.
type UpdateOptions struct {
	// Force skips user confirmation for destructive changes.
	Force bool
	// DryRun shows what would change without applying.
	DryRun bool
	// RollingReboot enables rolling reboots (one node at a time) for reboot-required changes.
	RollingReboot bool
}

// DefaultUpdateOptions returns sensible defaults for update operations.
func DefaultUpdateOptions() UpdateOptions {
	return UpdateOptions{
		Force:         false,
		DryRun:        false,
		RollingReboot: true,
	}
}

// PrepareUpdate handles the common update preamble shared by provisioners:
//   - If diffErr is non-nil, return it immediately.
//   - If dry-run, return the diff immediately.
//   - Create a mutable result from the diff.
//   - If recreate-required changes exist, return an error.
//
// Returns (result, true, nil) when the caller should continue applying changes.
// Returns (result, false, nil/err) when the caller should return result as-is.
func PrepareUpdate(
	diff *UpdateResult,
	diffErr error,
	opts UpdateOptions,
	recreateErr error,
) (*UpdateResult, bool, error) {
	if diffErr != nil {
		return nil, false, fmt.Errorf("failed to compute config diff: %w", diffErr)
	}

	if opts.DryRun {
		return diff, false, nil
	}

	result := NewUpdateResultFromDiff(diff)

	if diff.HasRecreateRequired() {
		return result, false, fmt.Errorf("%w: %d changes require recreation",
			recreateErr, len(diff.RecreateRequired))
	}

	return result, true, nil
}
