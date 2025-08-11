package helpers

import (
	"reflect"

	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
)

func NameInputOrFallback(ksailConfig *ksailcluster.Cluster, input string) string {
	return fallback(input, ksailConfig.Metadata.Name)
}

func DistributionInputOrFallback(ksailConfig *ksailcluster.Cluster, input ksailcluster.Distribution) ksailcluster.Distribution {
	return fallback(input, ksailConfig.Spec.Distribution)
}

func ReconciliationToolInputOrFallback(ksailConfig *ksailcluster.Cluster, input ksailcluster.ReconciliationTool) ksailcluster.ReconciliationTool {
	return fallback(input, ksailConfig.Spec.ReconciliationTool)
}

func ContainerEngineInputOrFallback(ksailConfig *ksailcluster.Cluster, input ksailcluster.ContainerEngine) ksailcluster.ContainerEngine {
	return fallback(input, ksailConfig.Spec.ContainerEngine)
}

// --- internals ---

// fallback returns input if not zero value, otherwise fallback.
func fallback[T comparable](input, fallback T) T {
	if !reflect.DeepEqual(input, *new(T)) {
		return input
	}
	return fallback
}