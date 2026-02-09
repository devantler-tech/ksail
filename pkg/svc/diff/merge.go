package diff

import (
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/types"
)

// clusterFieldPrefix is the prefix used by Engine for ClusterSpec-level fields.
// Provisioner diffs may omit this prefix — normalization strips it before dedup.
const clusterFieldPrefix = "cluster."

// MergeProvisionerDiff merges provisioner-specific diff results into the main diff.
// Provisioner diffs may contain distribution-specific changes (node counts, etc.)
// that the Engine doesn't track. We avoid duplicating fields already covered
// by Engine by checking field names.
func MergeProvisionerDiff(main, provisioner *types.UpdateResult) {
	if provisioner == nil {
		return
	}

	existingFields := collectExistingFields(main)

	main.InPlaceChanges = appendUniqueChanges(
		main.InPlaceChanges, provisioner.InPlaceChanges, existingFields,
	)
	main.RebootRequired = appendUniqueChanges(
		main.RebootRequired, provisioner.RebootRequired, existingFields,
	)
	main.RecreateRequired = appendUniqueChanges(
		main.RecreateRequired, provisioner.RecreateRequired, existingFields,
	)
}

// normalizeFieldName strips the "cluster." prefix for deduplication purposes,
// so "cluster.vanilla.mirrorsDir" and "vanilla.mirrorsDir" are treated as the same field.
func normalizeFieldName(field string) string {
	return strings.TrimPrefix(field, clusterFieldPrefix)
}

// collectExistingFields builds a set of normalized field names already present in the diff.
func collectExistingFields(d *types.UpdateResult) map[string]bool {
	changes := d.AllChanges()
	fields := make(map[string]bool, len(changes))

	for _, c := range changes {
		fields[normalizeFieldName(c.Field)] = true
	}

	return fields
}

// appendUniqueChanges appends changes from src to dst, skipping fields already in existing.
// Field names are normalized before comparison to avoid duplicates caused by prefix differences.
func appendUniqueChanges(dst, src []types.Change, existing map[string]bool) []types.Change {
	for _, c := range src {
		if !existing[normalizeFieldName(c.Field)] {
			dst = append(dst, c)
		}
	}

	return dst
}
