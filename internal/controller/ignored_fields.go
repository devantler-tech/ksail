package controller

import (
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// reasonIgnoredFieldsNone is the condition reason reported when no CLI-only fields are set.
const reasonIgnoredFieldsNone = "None"

// reasonIgnoredFieldsSet is the condition reason reported when CLI-only fields are set on the CR.
const reasonIgnoredFieldsSet = "CLIOnlyFieldsSet"

// ignoredCLIFields lists the spec fields the operator never reconciles: the Cluster type is shared
// between the ksail.yaml CLI configuration and the operator CRD, so a kubectl user can set fields
// that only the CLI honors. Each entry pairs the JSON path surfaced to the user with a predicate
// reporting whether the field is set on a given cluster.
//
//nolint:gochecknoglobals // immutable descriptor table for the CLI-only field probe
var ignoredCLIFields = []struct {
	path  string
	isSet func(*v1alpha1.Cluster) bool
}{
	{"spec.editor", func(c *v1alpha1.Cluster) bool { return c.Spec.Editor != "" }},
	{"spec.chat", func(c *v1alpha1.Cluster) bool { return c.Spec.Chat != v1alpha1.ChatSpec{} }},
	{
		"spec.cluster.connection",
		func(c *v1alpha1.Cluster) bool { return c.Spec.Cluster.Connection != v1alpha1.Connection{} },
	},
	{
		"spec.cluster.distributionConfig",
		func(c *v1alpha1.Cluster) bool { return c.Spec.Cluster.DistributionConfig != "" },
	},
	{
		"spec.workload.watch.hooks",
		func(c *v1alpha1.Cluster) bool { return len(c.Spec.Workload.Watch.Hooks) > 0 },
	},
}

// ignoredCLIFieldsSet returns the JSON paths of the CLI-only spec fields set on the cluster, in a
// stable order. An empty result means the CR sets no fields the operator ignores.
func ignoredCLIFieldsSet(cluster *v1alpha1.Cluster) []string {
	var set []string

	for _, field := range ignoredCLIFields {
		if field.isSet(cluster) {
			set = append(set, field.path)
		}
	}

	return set
}

// setIgnoredFieldsCondition records the IgnoredFields condition: False/None when the CR sets no
// CLI-only fields, True with the offending paths in the message otherwise. The condition is purely
// informational (it never affects Ready) and surfaces fields the operator silently accepts but does
// not reconcile, since the Cluster type is shared between the ksail.yaml CLI model and the CRD.
func (r *ClusterReconciler) setIgnoredFieldsCondition(cluster *v1alpha1.Cluster) {
	ignored := ignoredCLIFieldsSet(cluster)
	if len(ignored) == 0 {
		setCondition(
			cluster,
			v1alpha1.ConditionIgnoredFields,
			metav1.ConditionFalse,
			reasonIgnoredFieldsNone,
			"no CLI-only fields are set",
		)

		return
	}

	setCondition(
		cluster,
		v1alpha1.ConditionIgnoredFields,
		metav1.ConditionTrue,
		reasonIgnoredFieldsSet,
		"these CLI-only fields are set but ignored by the operator: "+strings.Join(ignored, ", "),
	)
}
