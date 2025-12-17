package k3dgenerator

import (
	"fmt"

	"github.com/devantler-tech/ksail-go/pkg/io"
	yamlgenerator "github.com/devantler-tech/ksail-go/pkg/io/generator/yaml"
	"github.com/devantler-tech/ksail-go/pkg/io/marshaller"
	yamlmarshaller "github.com/devantler-tech/ksail-go/pkg/io/marshaller/yaml"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
)

// K3dGenerator generates a k3d SimpleConfig YAML.
type K3dGenerator struct {
	Marshaller marshaller.Marshaller[*v1alpha5.SimpleConfig]
}

// NewK3dGenerator creates and returns a new K3dGenerator instance.
func NewK3dGenerator() *K3dGenerator {
	m := yamlmarshaller.NewMarshaller[*v1alpha5.SimpleConfig]()

	return &K3dGenerator{
		Marshaller: m,
	}
}

// Generate creates a k3d cluster YAML configuration and writes it to the specified output.
func (g *K3dGenerator) Generate(
	cluster *v1alpha5.SimpleConfig,
	opts yamlgenerator.Options,
) (string, error) {
	cluster.APIVersion = "k3d.io/v1alpha5"
	cluster.Kind = "Simple"

	out, err := g.Marshaller.Marshal(cluster)
	if err != nil {
		return "", fmt.Errorf("marshal k3d config: %w", err)
	}

	// write to file if output path is specified
	if opts.Output != "" {
		result, err := io.TryWriteFile(out, opts.Output, opts.Force)
		if err != nil {
			return "", fmt.Errorf("write k3d config: %w", err)
		}

		return result, nil
	}

	return out, nil
}
