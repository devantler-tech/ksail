// TODO: Fix `KustomizationGenerator` to not omit an empty resources list
package kustomizationGenerator

import (
	"fmt"
	"os"
	"path/filepath"

	"devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	yamlGenerator "devantler.tech/ksail/pkg/generator/yaml"
	yamlMarshaller "devantler.tech/ksail/pkg/marshaller/yaml"
	"sigs.k8s.io/kustomize/api/types"
)

// KustomizationGenerator is a generator for kustomization resources.
type KustomizationGenerator struct {
	Cluster    *cluster.Cluster
	Marshaller yamlMarshaller.YamlMarshaller[*types.Kustomization]
	Generator  yamlGenerator.YamlGenerator[types.Kustomization]
}

func (g *KustomizationGenerator) Generate(opts yamlGenerator.YamlGeneratorOptions) (string, error) {
	kustomization := types.Kustomization{
		TypeMeta: types.TypeMeta{
			APIVersion: "kustomize.config.k8s.io/v1beta1",
			Kind:       "Kustomization",
		},
		Resources: []string{},
	}
	outputFile := filepath.Join(opts.Output, "kustomization.yaml")
	if err := os.MkdirAll(filepath.Dir(outputFile), 0755); err != nil {
		fmt.Fprintln(os.Stderr, "\033[31m"+err.Error()+"\033[0m")
		os.Exit(1)
	}
	result, err := g.Generator.Generate(kustomization, yamlGenerator.YamlGeneratorOptions{Output: outputFile, Force: opts.Force})
	if err != nil {
		fmt.Fprintln(os.Stderr, "\033[31m"+err.Error()+"\033[0m")
		os.Exit(1)
	}
	return result, nil
}

func NewKustomizationGenerator(ksailConfig *cluster.Cluster) *KustomizationGenerator {
	marshaller := yamlMarshaller.NewYamlMarshaller[*types.Kustomization]()
	generator := yamlGenerator.NewYamlGenerator[types.Kustomization]()

	return &KustomizationGenerator{
		Cluster:    ksailConfig,
		Marshaller: *marshaller,
		Generator:  *generator,
	}
}
