package loader

// ConfigLoader is a generic interface implemented by all config loaders (ksail, kind, k3d)
// to allow polymorphic loading where the concrete type can be asserted by the caller.
type ConfigLoader interface {
    Load() (any, error)
}
