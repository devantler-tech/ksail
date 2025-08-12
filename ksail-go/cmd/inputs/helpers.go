package inputs

import (
	"reflect"

	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
)

func SetInputsOrFallback(cfg *ksailcluster.Cluster) {
	cfg.Metadata.Name = inputOrFallback(Name, cfg.Metadata.Name)
	cfg.Spec.ContainerEngine = inputOrFallback(ContainerEngine, cfg.Spec.ContainerEngine)
	cfg.Spec.Distribution = inputOrFallback(Distribution, cfg.Spec.Distribution)
	cfg.Spec.ReconciliationTool = inputOrFallback(ReconciliationTool, cfg.Spec.ReconciliationTool)
	cfg.Spec.SourceDirectory = inputOrFallback(SourceDirectory, cfg.Spec.SourceDirectory)
}

// --- internals ---

// inputOrFallback returns input if not zero value, otherwise InputOrFallback.
func inputOrFallback[T comparable](input, fallback T) T {
	if !reflect.DeepEqual(input, *new(T)) {
		return input
	}
	return fallback
}
