package yamlgenerator

import (
	"fmt"

	"github.com/devantler-tech/ksail-go/pkg/io"
	"github.com/devantler-tech/ksail-go/pkg/io/marshaller"
	yamlmarshaller "github.com/devantler-tech/ksail-go/pkg/io/marshaller/yaml"
)

// Options defines options for YAML generators when emitting files.
type Options struct {
	Output string // Output file path; if empty, only returns YAML without writing
	Force  bool   // Force overwrite existing files
}

// YAMLGenerator emits YAML for an arbitrary model using a provided marshaller.
type YAMLGenerator[T any] struct {
	Marshaller marshaller.Marshaller[T]
}

// NewYAMLGenerator creates a new YAMLGenerator instance.
func NewYAMLGenerator[T any]() *YAMLGenerator[T] {
	m := yamlmarshaller.NewMarshaller[T]()

	return &YAMLGenerator[T]{
		Marshaller: m,
	}
}

// Generate converts a model to YAML string format and optionally writes to file.
func (g *YAMLGenerator[T]) Generate(model T, opts Options) (string, error) {
	// marshal model
	modelYAML, err := g.Marshaller.Marshal(model)
	if err != nil {
		return "", fmt.Errorf("failed to marshal model to YAML: %w", err)
	}

	// write to file if output path is specified
	if opts.Output != "" {
		result, err := io.TryWriteFile(modelYAML, opts.Output, opts.Force)
		if err != nil {
			return "", fmt.Errorf("failed to write YAML to file: %w", err)
		}

		return result, nil
	}

	return modelYAML, nil
}
