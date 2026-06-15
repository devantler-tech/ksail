package generator

// Generator is implemented by specific distribution generators (kind, k3d, kustomization).
// The Options type parameter allows each implementation to define its own options structure.
type Generator[T any, Options any] interface {
	Generate(model T, opts Options) (string, error)
}
