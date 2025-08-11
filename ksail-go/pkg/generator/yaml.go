package generator

import (
	"github.com/devantler-tech/ksail/pkg/io"
	"github.com/devantler-tech/ksail/pkg/marshaller"
)

// Options defines options for generators when emitting files.
type Options struct {
	Output string
	Force  bool
}

// YAMLGenerator emits YAML for an arbitrary model using a provided marshaller.
type YAMLGenerator[T any] struct {
	io.FileWriter
	Marshaller marshaller.Marshaller[*T]
}

func (g *YAMLGenerator[T]) Generate(model T, opts Options) (string, error) {
	// marshal model
	modelYAML, err := g.Marshaller.Marshal(&model)
	if err != nil {
		return "", err
	}
	// write if requested
	return g.FileWriter.TryWrite(modelYAML, opts.Output, opts.Force)
}

// TryWrite writes content to opts.Output if set, handling force/overwrite messaging.
// TryWrite is still available on YAMLGenerator via the embedded FileWriter.

// NewYAMLGenerator creates a new YAMLGenerator instance.
func NewYAMLGenerator[T any]() *YAMLGenerator[T] {
	m := marshaller.NewMarshaller[*T]()
	return &YAMLGenerator[T]{Marshaller: m}
}
