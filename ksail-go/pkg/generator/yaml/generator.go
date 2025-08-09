package genyaml

import (
	"fmt"
	"os"

	yamlmarshal "devantler.tech/ksail/pkg/marshaller/yaml"
)

// YamlGeneratorOptions defines options for the YAML generator.
type YamlGeneratorOptions struct {
	Output string
	Force  bool
}

// YamlGenerator implements core.Generator for producing YAML output.
type YamlGenerator[T any] struct { Marshaller yamlmarshal.Marshaller[*T] }

func (g *YamlGenerator[T]) Generate(model T, options YamlGeneratorOptions) (string, error) {
	output := options.Output
	force := options.Force

	// marshal model
	modelYaml, err := g.Marshaller.Marshal(&model)
	if err != nil {
		return "", err
	}

	// process output
	if output != "" {
		// Check if file exists and we're not forcing
	if _, err := os.Stat(output); err == nil && !force {
			fmt.Printf("► skipping %s as it already exists, use --force to overwrite\n", output)
			return modelYaml, nil
		}

		// Determine the action message
		if _, err := os.Stat(output); err == nil {
			fmt.Printf("► overwriting '%s'\n", output)
		} else {
			fmt.Printf("► generating '%s'\n", output)
		}

		// Write the file
	if err := os.WriteFile(output, []byte(modelYaml), 0644); err != nil {
			return "", fmt.Errorf("failed to write file %s: %w", output, err)
		}
	}
	// return to caller
	return modelYaml, nil
}

// NewYamlGenerator creates a new YamlGenerator instance.
func NewYamlGenerator[T any]() *YamlGenerator[T] {
	marshaller := yamlmarshal.NewMarshaller[*T]()
	return &YamlGenerator[T]{
		Marshaller: *marshaller,
	}
}
