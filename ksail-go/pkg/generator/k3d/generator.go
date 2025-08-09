package genk3d

import (
	"fmt"

	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	yamlGenerator "devantler.tech/ksail/pkg/generator/yaml"
	yamlmarshal "devantler.tech/ksail/pkg/marshaller/yaml"
	"github.com/k3d-io/k3d/v5/pkg/config/types"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
)

// K3dGenerator is a generator for k3d resources.
type K3dGenerator struct {
	Cluster    *ksailcluster.Cluster
	Marshaller yamlmarshal.Marshaller[*v1alpha5.SimpleConfig]
	Generator  yamlGenerator.YamlGenerator[v1alpha5.SimpleConfig]
}

func (g *K3dGenerator) Generate(opts yamlGenerator.YamlGeneratorOptions) (string, error) {
	k3dCluster := v1alpha5.SimpleConfig{
		TypeMeta: types.TypeMeta{
			APIVersion: "k3d.io/v1alpha5",
			Kind:       "Simple",
		},
		ObjectMeta: types.ObjectMeta{
			Name: g.Cluster.Metadata.Name,
		},
	}

	result, err := g.Generator.Generate(k3dCluster, opts)
	if err != nil {
		return "", fmt.Errorf("failed to generate k3d config: %w", err)
	}

	return result, nil
}

func NewK3dGenerator(ksailConfig *ksailcluster.Cluster) *K3dGenerator {
	marshaller := yamlmarshal.NewMarshaller[*v1alpha5.SimpleConfig]()
	generator := yamlGenerator.NewYamlGenerator[v1alpha5.SimpleConfig]()

	return &K3dGenerator{
		Cluster:    ksailConfig,
		Marshaller: *marshaller,
		Generator:  *generator,
	}
}
