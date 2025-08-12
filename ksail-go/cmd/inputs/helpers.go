package inputs

import (
	"reflect"

	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
)

func SetInputsOrFallback(ksailConfig *ksailcluster.Cluster) {
	ksailConfig.Metadata.Name = inputOrFallback(Name, ksailConfig.Metadata.Name)
	ksailConfig.Spec.ContainerEngine = inputOrFallback(ContainerEngine, ksailConfig.Spec.ContainerEngine)
	ksailConfig.Spec.Distribution = inputOrFallback(Distribution, ksailConfig.Spec.Distribution)
	ksailConfig.Spec.ReconciliationTool = inputOrFallback(ReconciliationTool, ksailConfig.Spec.ReconciliationTool)
	ksailConfig.Spec.SourceDirectory = inputOrFallback(SourceDirectory, ksailConfig.Spec.SourceDirectory)
}

// --- internals ---

// inputOrFallback returns input if not zero value, otherwise InputOrFallback.
func inputOrFallback[T comparable](input, fallback T) T {
	if !reflect.DeepEqual(input, *new(T)) {
		return input
	}
	return fallback
}
