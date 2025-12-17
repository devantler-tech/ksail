package marshaller

// Marshaller is an interface for a resource marshaller.
type Marshaller[T any] interface {
	// Marshal serializes the model into a string representation.
	Marshal(model T) (string, error)

	// Unmarshal deserializes the model from a byte representation.
	Unmarshal(data []byte, model *T) error

	// UnmarshalString deserializes the model from a string representation.
	UnmarshalString(data string, model *T) error
}
