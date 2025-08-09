package marshaller

import (
	"sigs.k8s.io/yaml"
)

// YAMLMarshaller marshals/unmarshals YAML documents for a model type.
type YAMLMarshaller[T any] struct{}

// Marshal serializes the model into a string representation.
func (g *YAMLMarshaller[T]) Marshal(model T) (string, error) {
	data, err := yaml.Marshal(model)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// Unmarshal deserializes the model from a byte representation.
func (g *YAMLMarshaller[T]) Unmarshal(data []byte, model T) error {
	return yaml.Unmarshal(data, model)
}

// UnmarshalString deserializes the model from a string representation.
func (g *YAMLMarshaller[T]) UnmarshalString(data string, model T) error {
	return yaml.Unmarshal([]byte(data), model)
}

// NewMarshaller creates a new YAMLMarshaller instance implementing Marshaller.
func NewMarshaller[T any]() Marshaller[T] {
	return &YAMLMarshaller[T]{}
}
