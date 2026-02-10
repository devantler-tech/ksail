package k3dgenerator

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/yaml"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil/marshaller"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
)

// Generator generates a k3d SimpleConfig YAML.
type Generator struct {
	Marshaller marshaller.Marshaller[*v1alpha5.SimpleConfig]
}

// NewGenerator creates and returns a new Generator instance.
func NewGenerator() *Generator {
	m := marshaller.NewYAMLMarshaller[*v1alpha5.SimpleConfig]()

	return &Generator{
		Marshaller: m,
	}
}

// Generate creates a k3d cluster YAML configuration and writes it to the specified output.
func (g *Generator) Generate(
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
		result, err := fsutil.TryWriteFile(out, opts.Output, opts.Force)
		if err != nil {
			return "", fmt.Errorf("write k3d config: %w", err)
		}

		return result, nil
	}

	return out, nil
}
