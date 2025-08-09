// TODO: Implement `talosInDockerGenerator` to generate a simple talos config in a `talos/` directory
package talosInDockerGenerator

import (
	"os"

	color "devantler.tech/ksail/internal/util/fmt"
	"devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	yamlGenerator "devantler.tech/ksail/pkg/generator/yaml"
)

// TalosInDockerGenerator is a generator for kind resources.
type TalosInDockerGenerator struct {
	KSailConfig *cluster.Cluster
}

func (g *TalosInDockerGenerator) Generate(opts yamlGenerator.YamlGeneratorOptions) (string, error) {
  color.PrintError("TalosInDocker distribution is not yet implemented")
	os.Exit(1)

	return "", nil
}

func NewTalosInDockerGenerator(ksailConfig *cluster.Cluster) *TalosInDockerGenerator {
	return &TalosInDockerGenerator{
		KSailConfig: ksailConfig,
	}
}
