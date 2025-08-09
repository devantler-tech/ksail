package coreGenerator

// GeneratorOption represents a configuration option for the generator.
type GeneratorOption any

// Generator is an interface for a resource generator.
type Generator[T any] interface {
	// Generate creates a new resource from the provided model.
	Generate(model T, options ...GeneratorOption) (string, error)
}
