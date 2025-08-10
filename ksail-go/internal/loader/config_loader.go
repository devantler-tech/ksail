package loader

// ConfigLoader is a generic interface implemented by all config loaders (ksail, kind, k3d)
// to allow type-safe loading of concrete configuration models.
// Example:
//   var l ConfigLoader[*MyType]
//   cfg, err := l.Load()
//
// Implementations use concrete types via generics.
type ConfigLoader[T any] interface {
    Load() (T, error)
}
