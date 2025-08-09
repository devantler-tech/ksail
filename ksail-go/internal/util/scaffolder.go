package util

import (
	"os"
	"path/filepath"

	color "devantler.tech/ksail/internal/util/fmt"
	"devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	k3dGenerator "devantler.tech/ksail/pkg/generator/k3d"
	kindGenerator "devantler.tech/ksail/pkg/generator/kind"
	kustomizationGenerator "devantler.tech/ksail/pkg/generator/kustomization"
	talosInDockerGenerator "devantler.tech/ksail/pkg/generator/talos_in_docker"
	yamlGenerator "devantler.tech/ksail/pkg/generator/yaml"
)

type Scaffolder struct {
	KSailConfig            cluster.Cluster
	KSailYamlGenerator     *yamlGenerator.YamlGenerator[cluster.Cluster]
	KindGenerator          *kindGenerator.KindGenerator
	K3dGenerator           *k3dGenerator.K3dGenerator
	TalosInDockerGenerator *talosInDockerGenerator.TalosInDockerGenerator
	KustomizationGenerator *kustomizationGenerator.KustomizationGenerator
}

func (s *Scaffolder) Scaffold(output string, force bool) error {
	// generate ksail.yaml file
	_, err := s.KSailYamlGenerator.Generate(s.KSailConfig, yamlGenerator.YamlGeneratorOptions{Output: output + "ksail.yaml", Force: force})
	if err != nil {
		color.PrintError("%s", err)
		os.Exit(1)
	}

	// generate distribution config file
	switch s.KSailConfig.Spec.Distribution {
	case cluster.DistributionKind:
		s.KindGenerator.Generate(yamlGenerator.YamlGeneratorOptions{Output: output + "kind.yaml", Force: force})
	case cluster.DistributionK3d:
		s.K3dGenerator.Generate(yamlGenerator.YamlGeneratorOptions{Output: output + "k3d.yaml", Force: force})
	case cluster.DistributionTalosInDocker:
		s.TalosInDockerGenerator.Generate(yamlGenerator.YamlGeneratorOptions{Output: output + "talos-in-docker.yaml", Force: force})
	default:
		color.PrintError("provided distribution is unknown")
		os.Exit(1)
	}

	s.KustomizationGenerator.Generate(yamlGenerator.YamlGeneratorOptions{Output: filepath.Join(output, s.KSailConfig.Spec.SourceDirectory), Force: force})

	return nil
}

func NewScaffolder(ksailConfig cluster.Cluster) *Scaffolder {
	ksailGenerator := yamlGenerator.NewYamlGenerator[cluster.Cluster]()
	kindGenerator := kindGenerator.NewKindGenerator(&ksailConfig)
	k3dGenerator := k3dGenerator.NewK3dGenerator(&ksailConfig)
	talosInDockerGenerator := talosInDockerGenerator.NewTalosInDockerGenerator(&ksailConfig)
	kustomizationGenerator := kustomizationGenerator.NewKustomizationGenerator(&ksailConfig)

	return &Scaffolder{
		KSailConfig:            ksailConfig,
		KSailYamlGenerator:     ksailGenerator,
		KindGenerator:          kindGenerator,
		K3dGenerator:           k3dGenerator,
		TalosInDockerGenerator: talosInDockerGenerator,
		KustomizationGenerator: kustomizationGenerator,
	}
}
