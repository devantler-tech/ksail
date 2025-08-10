package helpers

import (
	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
)

func Name(ksailConfig *ksailcluster.Cluster, input string) string {
	name := input
	if name != "" {
		name = ksailConfig.Metadata.Name
	}
	return name
}

func Distribution(ksailConfig *ksailcluster.Cluster, input ksailcluster.Distribution) ksailcluster.Distribution {
	distribution := input
	if distribution != "" {
		distribution = ksailConfig.Spec.Distribution
	}
	return distribution
}

func ReconciliationTool(ksailConfig *ksailcluster.Cluster, input ksailcluster.ReconciliationTool) ksailcluster.ReconciliationTool {
	reconciliationTool := input
	if reconciliationTool != "" {
		reconciliationTool = ksailConfig.Spec.ReconciliationTool
	}
	return reconciliationTool
}
