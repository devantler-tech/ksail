package yamlmarshaller

import (
	"fmt"

	"github.com/devantler-tech/ksail-go/pkg/io/marshaller"
	"sigs.k8s.io/yaml"
)

// YAMLMarshaller marshals/unmarshals YAML documents for a model type.
type YAMLMarshaller[T any] struct{}

// NewMarshaller creates a new YAMLMarshaller instance implementing Marshaller.
func NewMarshaller[T any]() marshaller.Marshaller[T] {
	return &YAMLMarshaller[T]{}
}

// Marshal serializes the model into a string representation.
func (g *YAMLMarshaller[T]) Marshal(model T) (string, error) {
	data, err := yaml.Marshal(model)
	if err != nil {
		return "", fmt.Errorf("failed to marshal YAML: %w", err)
	}

	return string(data), nil
}

// Unmarshal deserializes the model from a byte representation.
func (g *YAMLMarshaller[T]) Unmarshal(data []byte, model *T) error {
	err := yaml.Unmarshal(data, model)
	if err != nil {
		return fmt.Errorf("failed to unmarshal YAML: %w", err)
	}

	return nil
}

// UnmarshalString deserializes the model from a string representation.
func (g *YAMLMarshaller[T]) UnmarshalString(data string, model *T) error {
	err := yaml.Unmarshal([]byte(data), model)
	if err != nil {
		return fmt.Errorf("failed to unmarshal YAML string: %w", err)
	}

	return nil
}
