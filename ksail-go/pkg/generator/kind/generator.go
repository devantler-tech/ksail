package kindGenerator

import (
	"fmt"
	"os"

	"devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	yamlGenerator "devantler.tech/ksail/pkg/generator/yaml"
	yamlMarshaller "devantler.tech/ksail/pkg/marshaller/yaml"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// KindGenerator is a generator for kind resources.
type KindGenerator struct {
	Cluster    *cluster.Cluster
	Marshaller yamlMarshaller.YamlMarshaller[*v1alpha4.Cluster]
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
		fmt.Fprintln(os.Stderr, "\033[31m"+err.Error()+"\033[0m")
		os.Exit(1)
	}

	return result, nil
}

func NewKindGenerator(ksailConfig *cluster.Cluster) *KindGenerator {
	marshaller := yamlMarshaller.NewYamlMarshaller[*v1alpha4.Cluster]()
	generator := yamlGenerator.NewYamlGenerator[v1alpha4.Cluster]()

	return &KindGenerator{
		Cluster:    ksailConfig,
		Marshaller: *marshaller,
		Generator:  *generator,
	}
}
