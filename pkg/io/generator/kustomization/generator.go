package kustomizationgenerator

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail-go/pkg/io"
	yamlgenerator "github.com/devantler-tech/ksail-go/pkg/io/generator/yaml"
	"github.com/devantler-tech/ksail-go/pkg/io/marshaller"
	yamlmarshaller "github.com/devantler-tech/ksail-go/pkg/io/marshaller/yaml"
	ktypes "sigs.k8s.io/kustomize/api/types"
)

// KustomizationGenerator generates a kustomization.yaml.
type KustomizationGenerator struct {
	Marshaller marshaller.Marshaller[*ktypes.Kustomization]
}

// NewKustomizationGenerator creates and returns a new KustomizationGenerator instance.
func NewKustomizationGenerator() *KustomizationGenerator {
	m := yamlmarshaller.NewMarshaller[*ktypes.Kustomization]()

	return &KustomizationGenerator{
		Marshaller: m,
	}
}

// Generate creates a kustomization.yaml file and writes it to the specified output file path.
func (g *KustomizationGenerator) Generate(
	kustomization *ktypes.Kustomization,
	opts yamlgenerator.Options,
) (string, error) {
	kustomization.TypeMeta = ktypes.TypeMeta{
		APIVersion: "kustomize.config.k8s.io/v1beta1",
		Kind:       "Kustomization",
	}
	kustomization.Resources = []string{}

	out, err := g.Marshaller.Marshal(kustomization)
	if err != nil {
		return "", fmt.Errorf("marshal kustomization: %w", err)
	}

	// Only add resources: [] if no resources field is present at all
	if !strings.Contains(out, "resources:") {
		out += "resources: []\n"
	}

	// If no output file specified, just return the YAML
	if opts.Output == "" {
		return out, nil
	}

	result, err := io.TryWriteFile(out, opts.Output, opts.Force)
	if err != nil {
		return "", fmt.Errorf("write kustomization: %w", err)
	}

	return result, nil
}
