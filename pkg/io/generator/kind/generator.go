package kindgenerator

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/fileutil"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/yaml"
	"github.com/devantler-tech/ksail/v5/pkg/io/marshaller"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// KindGenerator generates a kind Cluster YAML.
type KindGenerator struct {
	Marshaller marshaller.Marshaller[*v1alpha4.Cluster]
}

// NewKindGenerator creates and returns a new KindGenerator instance.
func NewKindGenerator() *KindGenerator {
	m := marshaller.NewYAMLMarshaller[*v1alpha4.Cluster]()

	return &KindGenerator{
		Marshaller: m,
	}
}

// Generate creates a kind cluster YAML configuration and writes it to the specified output.
func (g *KindGenerator) Generate(
	cfg *v1alpha4.Cluster,
	opts yamlgenerator.Options,
) (string, error) {
	// Ensure APIVersion and Kind are set before applying defaults
	cfg.APIVersion = "kind.x-k8s.io/v1alpha4"
	cfg.Kind = "Cluster"

	out, err := g.Marshaller.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal kind config: %w", err)
	}

	// write to file if output path is specified
	if opts.Output != "" {
		result, err := fileutil.TryWriteFile(out, opts.Output, opts.Force)
		if err != nil {
			return "", fmt.Errorf("write kind config: %w", err)
		}

		return result, nil
	}

	return out, nil
}
