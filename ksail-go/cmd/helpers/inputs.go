package helpers

import (
	"reflect"

	"github.com/devantler-tech/ksail/cmd/inputs"
	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
)

func SetInputsOrFallback(ksailConfig *ksailcluster.Cluster) {
	ksailConfig.Metadata.Name = inputOrFallback(inputs.Name, ksailConfig.Metadata.Name)
	ksailConfig.Spec.ContainerEngine = inputOrFallback(inputs.ContainerEngine, ksailConfig.Spec.ContainerEngine)
	ksailConfig.Spec.Distribution = inputOrFallback(inputs.Distribution, ksailConfig.Spec.Distribution)
	ksailConfig.Spec.ReconciliationTool = inputOrFallback(inputs.ReconciliationTool, ksailConfig.Spec.ReconciliationTool)
	ksailConfig.Spec.SourceDirectory = inputOrFallback(inputs.SourceDirectory, ksailConfig.Spec.SourceDirectory)
}

// --- internals ---

// inputOrFallback returns input if not zero value, otherwise InputOrFallback.
func inputOrFallback[T comparable](input, fallback T) T {
	if !reflect.DeepEqual(input, *new(T)) {
		return input
	}
	return fallback
}
