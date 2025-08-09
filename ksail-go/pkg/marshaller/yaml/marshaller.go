package yamlMarshaller

import (
	"sigs.k8s.io/yaml"
)

// YAMLMarshaller is a struct for marshalling YAML resources.
type YamlMarshaller[T any] struct {
}

// Marshal serializes the model into a string representation.
func (g *YamlMarshaller[T]) Marshal(model T) (string, error) {
	data, err := yaml.Marshal(model)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// Unmarshal deserializes the model from a byte representation.
func (g *YamlMarshaller[T]) Unmarshal(data []byte, model T) error {
	return yaml.Unmarshal(data, model)
}

func (g *YamlMarshaller[T]) UnmarshalString(data string, model T) error {
	return yaml.Unmarshal([]byte(data), model)
}

// NewYamlMarshaller creates a new YamlMarshaller instance.
func NewYamlMarshaller[T any]() *YamlMarshaller[T] {
	return &YamlMarshaller[T]{}
}
