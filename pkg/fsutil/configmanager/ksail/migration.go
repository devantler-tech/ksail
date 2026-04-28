package configmanager

import (
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// migrateDeprecatedNodeCounts copies legacy spec.cluster.talos.{controlPlanes,workers}
// into the cluster-level spec.cluster.{controlPlanes,workers} when the new fields are
// unset. Emits a deprecation notice to the supplied writer the first time a legacy
// value is observed.
//
// Migration rules per field:
//   - new == 0 && old != 0 → copy old → new, warn.
//   - new != 0 && old != 0 && old == new → silently zero old (no warning, no copy needed).
//   - new != 0 && old != 0 && old != new → return an error (ambiguous configuration).
//   - new != 0 && old == 0 → no-op (current canonical path).
//   - new == 0 && old == 0 → no-op (downstream defaults will fill it in).
func migrateDeprecatedNodeCounts(cfg *v1alpha1.Cluster, out io.Writer) error {
	if cfg == nil {
		return nil
	}

	cluster := &cfg.Spec.Cluster
	talos := &cluster.Talos

	if err := migrateDeprecatedInt32(
		&cluster.ControlPlanes,
		&talos.ControlPlanes,
		"spec.cluster.talos.controlPlanes",
		"spec.cluster.controlPlanes",
		out,
	); err != nil {
		return err
	}

	return migrateDeprecatedInt32(
		&cluster.Workers,
		&talos.Workers,
		"spec.cluster.talos.workers",
		"spec.cluster.workers",
		out,
	)
}

func migrateDeprecatedInt32(newField, oldField *int32, oldPath, newPath string, out io.Writer) error {
	if *oldField == 0 {
		return nil
	}

	if *newField != 0 && *newField != *oldField {
		return fmt.Errorf(
			"%w: %s=%d conflicts with %s=%d (set only %s)",
			ErrDeprecatedFieldConflict,
			oldPath, *oldField, newPath, *newField, newPath,
		)
	}

	copied := *newField == 0
	if copied {
		*newField = *oldField
	}

	*oldField = 0

	if copied && out != nil {
		_, _ = fmt.Fprintf(
			out,
			"warning: %s is deprecated; use %s. KSail migrated the value automatically.\n",
			oldPath, newPath,
		)
	}

	return nil
}
