// Package types provides shared types for cluster provisioner operations.
// These are separated to avoid import cycles between provisioner implementations
// and the main provisioner interface package.
//
//nolint:revive // package name "types" is intentionally generic for shared types
package types

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
