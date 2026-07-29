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
// newFieldExplicit must be true when spec.cluster.autoscaler.node.enabled was explicitly
// set by the user (e.g. via Viper.IsSet). When false, a zero new field is treated as
// "unset" and the legacy value is copied over with a deprecation warning.
//
// Migration rules (the new field is now the NodeAutoscalerEnabled toggle enum;
// its zero value is the empty string, treated as Disabled by IsEnabled):
//   - old is empty → no-op (canonical or already-migrated path).
//   - old non-empty && newFieldExplicit=true  && new==mapped → equivalent; silently zero old.
//   - old non-empty && newFieldExplicit=true  && new!=mapped → conflict; return error.
//   - old non-empty && newFieldExplicit=false → copy mapping to new, zero old, warn.
//
// mapNodeAutoscalingToEnabled maps the deprecated NodeAutoscaling enum to the new
// NodeAutoscalerEnabled toggle enum. Returns an error if the legacy value is not
// one of the recognized NodeAutoscaling values.
func mapNodeAutoscalingToEnabled(
	old v1alpha1.NodeAutoscaling,
) (v1alpha1.NodeAutoscalerEnabled, error) {
	switch old {
	case v1alpha1.NodeAutoscalingEnabled:
		return v1alpha1.NodeAutoscalerEnabledEnabled, nil
	case v1alpha1.NodeAutoscalingDisabled:
		return v1alpha1.NodeAutoscalerEnabledDisabled, nil
	default:
		return "", fmt.Errorf(
			"%w: spec.cluster.nodeAutoscaling=%s is not a recognized value "+
				"(valid options: %s, %s)",
			v1alpha1.ErrInvalidNodeAutoscaling,
			old,
			v1alpha1.NodeAutoscalingEnabled,
			v1alpha1.NodeAutoscalingDisabled,
		)
	}
}

func migrateDeprecatedNodeAutoscaling(
	cfg *v1alpha1.Cluster,
	newFieldExplicit bool,
	out io.Writer,
) error {
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

	// Conflict: new field was explicitly set and disagrees with the legacy value.
	if newFieldExplicit && *newField != mapped {
		return fmt.Errorf(
			"%w: spec.cluster.nodeAutoscaling=%s conflicts with "+
				"spec.cluster.autoscaler.node.enabled=%s "+
				"(set only spec.cluster.autoscaler.node.enabled)",
			ErrDeprecatedFieldConflict,
			*old,
			*newField,
		)
	}

	// Only copy and warn when the new field was not explicitly set.
	copied := !newFieldExplicit
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

// migrateDeprecatedImageVerification copies the legacy Talos-scoped
// spec.cluster.talos.imageVerification into the promoted cluster-level
// spec.cluster.imageVerification (which now steers Kind/K3s behavior too) when the
// new field is unset. Emits a deprecation notice to the supplied writer when a
// legacy value is copied into the new field.
//
// Migration rules (string enum; Go zero value is ""):
//   - old == ""                              → no-op (canonical or already-migrated path).
//   - old != "" && new == ""                 → copy old → new, zero old, warn.
//   - old != "" && new != "" && old == new   → silently zero old (equivalent).
//   - old != "" && new != "" && old != new   → return an error (ambiguous configuration).
func migrateDeprecatedImageVerification(cfg *v1alpha1.Cluster, out io.Writer) error {
	if cfg == nil {
		return nil
	}

	cluster := &cfg.Spec.Cluster
	//nolint:staticcheck // intentional: migration of deprecated field
	old := &cluster.Talos.ImageVerification
	newField := &cluster.ImageVerification

	if *old == "" {
		return nil
	}

	if *newField != "" && *newField != *old {
		return fmt.Errorf(
			"%w: spec.cluster.talos.imageVerification=%s conflicts with "+
				"spec.cluster.imageVerification=%s (set only spec.cluster.imageVerification)",
			ErrDeprecatedFieldConflict,
			*old, *newField,
		)
	}

	copied := *newField == ""
	if copied {
		*newField = *old
	}

	*old = ""

	if copied && out != nil {
		_, _ = fmt.Fprintf(
			out,
			"warning: spec.cluster.talos.imageVerification is deprecated; "+
				"use spec.cluster.imageVerification. KSail migrated the value automatically.\n",
		)
	}

	return nil
}

// warnDeprecatedTalosPatchFields emits deprecation warnings for ksail.yaml fields
// that wrap native Talos machine config patches. These fields are deprecated for Talos
// clusters; users should manage patches directly in the talos/ patch directories.
func warnDeprecatedTalosPatchFields(cfg *v1alpha1.Cluster, out io.Writer) {
	if cfg == nil || out == nil {
		return
	}

	if cfg.Spec.Cluster.Distribution != v1alpha1.DistributionTalos {
		return
	}

	warnCDIDeprecation(cfg, out)
	warnOIDCDeprecation(cfg, out)
	warnIngressFirewallDeprecation(cfg, out)
}

func warnCDIDeprecation(cfg *v1alpha1.Cluster, out io.Writer) {
	if cfg.Spec.Cluster.CDI == "" || cfg.Spec.Cluster.CDI == v1alpha1.CDIDefault {
		return
	}

	_, _ = fmt.Fprintf(out,
		"warning: spec.cluster.cdi is deprecated for Talos clusters. "+
			"Manage CDI via native Talos patches in talos/cluster/ instead "+
			"(e.g., machine.features.enableCDI in disable-cdi.yaml).\n")
}

func warnOIDCDeprecation(cfg *v1alpha1.Cluster, out io.Writer) {
	if !cfg.Spec.Cluster.OIDC.Enabled() {
		return
	}

	_, _ = fmt.Fprintf(out,
		"warning: spec.cluster.oidc is deprecated for Talos clusters. "+
			"Configure OIDC via native Talos patches in talos/cluster/oidc.yaml instead "+
			"(KubeAuthenticationConfig on Talos 1.14+; "+
			"cluster.apiServer.extraArgs on older releases).\n")
}

func warnIngressFirewallDeprecation(cfg *v1alpha1.Cluster, out io.Writer) {
	if cfg.Spec.Cluster.Provider != v1alpha1.ProviderHetzner {
		return
	}

	if cfg.Spec.Provider.Hetzner.IngressFirewall != v1alpha1.IngressFirewallDisabled {
		return
	}

	_, _ = fmt.Fprintf(out,
		"warning: spec.provider.hetzner.ingressFirewall is deprecated for Talos clusters. "+
			"To disable the ingress firewall, remove the firewall patch files from "+
			"talos/cluster/, talos/control-planes/, and talos/workers/ instead.\n")
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
