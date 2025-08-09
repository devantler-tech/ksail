package util

import (
	"fmt"
	"path/filepath"

	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	k3dgen "devantler.tech/ksail/pkg/generator/k3d"
	kindgen "devantler.tech/ksail/pkg/generator/kind"
	kustgen "devantler.tech/ksail/pkg/generator/kustomization"
	talosdockergen "devantler.tech/ksail/pkg/generator/tind"
	yamlgen "devantler.tech/ksail/pkg/generator/yaml"
)

type Scaffolder struct {
	KSailConfig            ksailcluster.Cluster
	KSailYamlGenerator     *yamlgen.YamlGenerator[ksailcluster.Cluster]
	KindGenerator          *kindgen.KindGenerator
	K3dGenerator           *k3dgen.K3dGenerator
	TindGenerator *talosdockergen.TindGenerator
	KustomizationGenerator *kustgen.KustomizationGenerator
}

func (s *Scaffolder) Scaffold(output string, force bool) error {
	// generate ksail.yaml file
	_, err := s.KSailYamlGenerator.Generate(s.KSailConfig, yamlgen.YamlGeneratorOptions{Output: output + "ksail.yaml", Force: force})
	if err != nil {
		return err
	}

	// generate distribution config file
	switch s.KSailConfig.Spec.Distribution {
	case ksailcluster.DistributionKind:
	if _, err := s.KindGenerator.Generate(yamlgen.YamlGeneratorOptions{Output: output + "kind.yaml", Force: force}); err != nil {
			return err
		}
	case ksailcluster.DistributionK3d:
	if _, err := s.K3dGenerator.Generate(yamlgen.YamlGeneratorOptions{Output: output + "k3d.yaml", Force: force}); err != nil {
			return err
		}
	case ksailcluster.DistributionTind:
	if _, err := s.TindGenerator.Generate(yamlgen.YamlGeneratorOptions{Output: output + "talos-in-docker.yaml", Force: force}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("provided distribution is unknown")
	}

	if _, err := s.KustomizationGenerator.Generate(yamlgen.YamlGeneratorOptions{Output: filepath.Join(output, s.KSailConfig.Spec.SourceDirectory), Force: force}); err != nil {
		return err
	}

	return nil
}

func NewScaffolder(ksailConfig ksailcluster.Cluster) *Scaffolder {
	ksailGen := yamlgen.NewYamlGenerator[ksailcluster.Cluster]()
	kindGen := kindgen.NewKindGenerator(&ksailConfig)
	k3dGen := k3dgen.NewK3dGenerator(&ksailConfig)
	talosDockerGen := talosdockergen.NewTindGenerator(&ksailConfig)
	kustGen := kustgen.NewKustomizationGenerator(&ksailConfig)

	return &Scaffolder{
		KSailConfig:            ksailConfig,
		KSailYamlGenerator:     ksailGen,
		KindGenerator:          kindGen,
		K3dGenerator:           k3dGen,
		TindGenerator: talosDockerGen,
		KustomizationGenerator: kustGen,
	}
}
