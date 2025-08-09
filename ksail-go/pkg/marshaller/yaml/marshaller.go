package marshalleryaml

import (
	"sigs.k8s.io/yaml"
)

// Marshaller marshals/unmarshals YAML documents for a model type.
type Marshaller[T any] struct{}

// Marshal serializes the model into a string representation.
func (g *Marshaller[T]) Marshal(model T) (string, error) {
	data, err := yaml.Marshal(model)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// Unmarshal deserializes the model from a byte representation.
func (g *Marshaller[T]) Unmarshal(data []byte, model T) error {
	return yaml.Unmarshal(data, model)
}

func (g *Marshaller[T]) UnmarshalString(data string, model T) error {
	return yaml.Unmarshal([]byte(data), model)
}

// NewMarshaller creates a new Marshaller instance.
func NewMarshaller[T any]() *Marshaller[T] {
	return &Marshaller[T]{}
}
