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
// Migration rules:
//   - new is empty && old is non-empty → copy mapping to new, zero old, warn.
//   - new is non-empty && old is non-empty && equivalent → silently zero old.
//   - new is non-empty && old is non-empty && different → return error (ambiguous).
//   - new is non-empty && old is empty → no-op (canonical path).
//   - both empty → no-op.
//
// mapNodeAutoscalingToEnabled maps the deprecated NodeAutoscaling enum to NodeAutoscalerEnabled.
func mapNodeAutoscalingToEnabled(old v1alpha1.NodeAutoscaling) v1alpha1.NodeAutoscalerEnabled {
	if old == v1alpha1.NodeAutoscalingEnabled {
		return v1alpha1.NodeAutoscalerEnabledEnabled
	}

	return v1alpha1.NodeAutoscalerEnabledDisabled
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

	mapped := mapNodeAutoscalingToEnabled(*old)

	if *newField != "" && *newField != mapped {
		return fmt.Errorf(
			"%w: spec.cluster.nodeAutoscaling=%s conflicts with "+
				"spec.cluster.autoscaler.node.enabled=%s "+
				"(set only spec.cluster.autoscaler.node.enabled)",
			ErrDeprecatedFieldConflict,
			*old,
			*newField,
		)
	}

	copied := *newField == ""
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
