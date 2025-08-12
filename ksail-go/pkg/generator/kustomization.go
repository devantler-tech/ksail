package generator

import (
	"fmt"
	"os"
	"path/filepath"

	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
	"github.com/devantler-tech/ksail/pkg/io"
	"github.com/devantler-tech/ksail/pkg/marshaller"
	ktypes "sigs.k8s.io/kustomize/api/types"
)

// KustomizationGenerator generates a kustomization.yaml.
type KustomizationGenerator struct {
	io.FileWriter
	KSailConfig    *ksailcluster.Cluster
	Marshaller marshaller.Marshaller[*ktypes.Kustomization]
}

func (g *KustomizationGenerator) Generate(opts Options) (string, error) {
	kustomization := ktypes.Kustomization{
		TypeMeta:  ktypes.TypeMeta{APIVersion: "kustomize.config.k8s.io/v1beta1", Kind: "Kustomization"},
		Resources: []string{},
	}
	outputFile := filepath.Join(opts.Output, "kustomization.yaml")
	if err := os.MkdirAll(filepath.Dir(outputFile), 0755); err != nil {
		return "", fmt.Errorf("create kustomization dir: %w", err)
	}
	out, err := g.Marshaller.Marshal(&kustomization)
	if err != nil {
		return "", fmt.Errorf("marshal kustomization: %w", err)
	}
	return g.FileWriter.TryWrite(out, outputFile, opts.Force)
}

func NewKustomizationGenerator(cfg *ksailcluster.Cluster) *KustomizationGenerator {
	return &KustomizationGenerator{
		KSailConfig:    cfg,
		Marshaller: marshaller.NewMarshaller[*ktypes.Kustomization](),
	}
}
