// Package marshaller provides functionality for marshaling and unmarshaling resources.
//
// This package defines the Marshaller interface for serializing and deserializing
// Go structs to and from various formats (YAML, JSON, etc.).
//
// Key functionality:
//   - Marshaller[T]: Generic interface for serialization/deserialization
//   - Marshal: Serialize model to string
//   - Unmarshal: Deserialize from bytes to model
//   - UnmarshalString: Deserialize from string to model
//
// Subpackages:
//   - yaml: YAML marshaller implementation
package marshaller
