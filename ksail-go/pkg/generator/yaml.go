package generator

import (
	"devantler.tech/ksail/pkg/marshaller"
)

// YamlGenerator emits YAML for an arbitrary model using a provided marshaller.
type YamlGenerator[T any] struct{ Marshaller marshaller.Marshaller[*T] }

func (g *YamlGenerator[T]) Generate(model T, opts Options) (string, error) {
	// marshal model
	modelYaml, err := g.Marshaller.Marshal(&model)
	if err != nil {
		return "", err
	}
	// write if requested
	return writeMaybe(modelYaml, opts)
}

// NewYamlGenerator creates a new YamlGenerator instance.
func NewYamlGenerator[T any]() *YamlGenerator[T] {
	m := marshaller.NewMarshaller[*T]()
	return &YamlGenerator[T]{Marshaller: m}
}
