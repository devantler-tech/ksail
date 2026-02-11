// Package diff provides a DiffEngine that computes configuration differences
// between old and new ClusterSpec values and classifies their update impact
// into in-place, reboot-required, and recreate-required categories.
//
// It also provides helpers for merging provisioner-specific diffs into a
// single UpdateResult, deduplicating overlapping field names.
package diff
