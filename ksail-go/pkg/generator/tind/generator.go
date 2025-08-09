package tindgen

import (
	"errors"

	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	yamlGenerator "devantler.tech/ksail/pkg/generator/yaml"
)

// TindGenerator is a generator for kind resources.
type TindGenerator struct {
	KSailConfig *ksailcluster.Cluster
}

func (g *TindGenerator) Generate(opts yamlGenerator.YamlGeneratorOptions) (string, error) {
	return "", errors.New("talos-in-docker distribution is not yet implemented")
}

func NewTindGenerator(ksailConfig *ksailcluster.Cluster) *TindGenerator {
	return &TindGenerator{
		KSailConfig: ksailConfig,
	}
}
