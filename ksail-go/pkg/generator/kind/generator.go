package genkind

import (
	"fmt"

	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	yamlGenerator "devantler.tech/ksail/pkg/generator/yaml"
	yamlmarshal "devantler.tech/ksail/pkg/marshaller/yaml"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// KindGenerator is a generator for kind resources.
type KindGenerator struct {
	Cluster    *ksailcluster.Cluster
	Marshaller yamlmarshal.Marshaller[*v1alpha4.Cluster]
	Generator  yamlGenerator.YamlGenerator[v1alpha4.Cluster]
}

func (g *KindGenerator) Generate(opts yamlGenerator.YamlGeneratorOptions) (string, error) {
	kindCluster := v1alpha4.Cluster{
		TypeMeta: v1alpha4.TypeMeta{
			APIVersion: "kind.x-k8s.io/v1alpha4",
			Kind:       "Cluster",
		},
	}
	v1alpha4.SetDefaultsCluster(&kindCluster)
	result, err := g.Generator.Generate(kindCluster, opts)
	if err != nil {
		return "", fmt.Errorf("generate kind config: %w", err)
	}
	return result, nil
}

func NewKindGenerator(ksailConfig *ksailcluster.Cluster) *KindGenerator {
	marshaller := yamlmarshal.NewMarshaller[*v1alpha4.Cluster]()
	generator := yamlGenerator.NewYamlGenerator[v1alpha4.Cluster]()

	return &KindGenerator{
		Cluster:    ksailConfig,
		Marshaller: *marshaller,
		Generator:  *generator,
	}
}
