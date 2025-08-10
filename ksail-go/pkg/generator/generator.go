package generator

// Generator is implemented by specific distribution generators (kind, k3d, kustomization).
type Generator interface {
	Generate(any) (string, error)
}
