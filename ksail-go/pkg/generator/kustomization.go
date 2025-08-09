package generator

import (
	"fmt"
	"os"
	"path/filepath"

	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	"devantler.tech/ksail/pkg/marshaller"
	ktypes "sigs.k8s.io/kustomize/api/types"
)

// KustomizationGenerator generates a kustomization.yaml.
type KustomizationGenerator struct {
	Cluster    *ksailcluster.Cluster
	Marshaller marshaller.Marshaller[*ktypes.Kustomization]
}

func (g *KustomizationGenerator) Generate(opts Options) (string, error) {
	kustomization := ktypes.Kustomization{
		TypeMeta:  ktypes.TypeMeta{APIVersion: "kustomize.config.k8s.io/v1beta1", Kind: "Kustomization"},
		Resources: []string{},
	}
	// Resolve output directory path for kustomization.yaml
	outputFile := opts.Output
	if outputFile == "" {
		outputFile = filepath.Join(".", "kustomization.yaml")
	}
	if err := os.MkdirAll(filepath.Dir(outputFile), 0755); err != nil {
		return "", fmt.Errorf("create kustomization dir: %w", err)
	}
	out, err := g.Marshaller.Marshal(&kustomization)
	if err != nil {
		return "", fmt.Errorf("marshal kustomization: %w", err)
	}
	return writeMaybe(out, Options{Output: outputFile, Force: opts.Force})
}

func NewKustomizationGenerator(ksailConfig *ksailcluster.Cluster) *KustomizationGenerator {
	return &KustomizationGenerator{
		Cluster:    ksailConfig,
		Marshaller: marshaller.NewMarshaller[*ktypes.Kustomization](),
	}
}
