package configmanager

import (
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// migrateDeprecatedNodeCounts copies legacy spec.cluster.talos.{controlPlanes,workers}
// into the cluster-level spec.cluster.{controlPlanes,workers} when the new fields are
// unset. Emits a deprecation notice to the supplied writer when a legacy value is
// copied into the new field.
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

	err := migrateDeprecatedInt32(
		&cluster.ControlPlanes,
		&talos.ControlPlanes, //nolint:staticcheck // intentional: migration of deprecated field
		"spec.cluster.talos.controlPlanes",
		"spec.cluster.controlPlanes",
		out,
	)
	if err != nil {
		return err
	}

	return migrateDeprecatedInt32(
		&cluster.Workers,
		&talos.Workers, //nolint:staticcheck // intentional: migration of deprecated field
		"spec.cluster.talos.workers",
		"spec.cluster.workers",
		out,
	)
}

// migrateDeprecatedNodeAutoscaling migrates the legacy spec.cluster.nodeAutoscaling field
// into spec.cluster.autoscaler.node.enabled when the new field is unset.
// Emits a deprecation notice to the supplied writer when a legacy value is migrated.
//
// Migration rules (bool semantics — Go zero value for bool is false):
//   - old is empty → no-op (canonical or already-migrated path).
//   - old is non-empty && new=true  && mapped=true  → equivalent; silently zero old.
//   - old is non-empty && new=true  && mapped=false → conflict; return error.
//   - old is non-empty && new=false → copy mapping to new, zero old, warn.
//     Note: because Go's bool zero value is false, "new=false" cannot distinguish
//     "unset" from "explicitly false", so the warning is always emitted when old is
//     set regardless of whether new was intentionally false (e.g. Disabled→false).
//
// mapNodeAutoscalingToEnabled maps the deprecated NodeAutoscaling enum to a bool.
// Returns an error if the legacy value is not one of the recognized NodeAutoscaling values.
func mapNodeAutoscalingToEnabled(
	old v1alpha1.NodeAutoscaling,
) (bool, error) {
	switch old {
	case v1alpha1.NodeAutoscalingEnabled:
		return true, nil
	case v1alpha1.NodeAutoscalingDisabled:
		return false, nil
	default:
		return false, fmt.Errorf(
			"%w: spec.cluster.nodeAutoscaling=%s is not a recognized value "+
				"(valid options: %s, %s)",
			v1alpha1.ErrInvalidNodeAutoscaling,
			old,
			v1alpha1.NodeAutoscalingEnabled,
			v1alpha1.NodeAutoscalingDisabled,
		)
	}
}

func migrateDeprecatedNodeAutoscaling(cfg *v1alpha1.Cluster, out io.Writer) error {
	if cfg == nil {
		return nil
	}

	old := &cfg.Spec.Cluster.NodeAutoscaling
	newField := &cfg.Spec.Cluster.Autoscaler.Node.Enabled

	if *old == "" {
		return nil
	}

	mapped, err := mapNodeAutoscalingToEnabled(*old)
	if err != nil {
		return err
	}

	// Conflict: newField is explicitly true but legacy says Disabled.
	// Note: the reverse case (newField=false, mapped=true) cannot be detected
	// because Go's bool zero value is false — we cannot distinguish "unset" from
	// "explicitly false". In that case the migration silently copies mapped=true
	// into newField, which is the safest behaviour (preserves the legacy intent).
	if *newField && !mapped {
		return fmt.Errorf(
			"%w: spec.cluster.nodeAutoscaling=%s conflicts with "+
				"spec.cluster.autoscaler.node.enabled=true "+
				"(set only spec.cluster.autoscaler.node.enabled)",
			ErrDeprecatedFieldConflict,
			*old,
		)
	}

	copied := !*newField
	if copied {
		*newField = mapped
	}

	*old = ""

	if copied && out != nil {
		_, _ = fmt.Fprintf(
			out,
			"warning: spec.cluster.nodeAutoscaling is deprecated; "+
				"use spec.cluster.autoscaler.node.enabled. "+
				"KSail migrated the value automatically.\n",
		)
	}

	return nil
}

func migrateDeprecatedInt32(
	newField, oldField *int32,
	oldPath, newPath string,
	out io.Writer,
) error {
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
