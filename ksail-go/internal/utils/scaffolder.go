package utils

import (
	"fmt"
	"path/filepath"

	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
	gen "github.com/devantler-tech/ksail/pkg/generator"
)

type Scaffolder struct {
	KSailConfig            ksailcluster.Cluster
	KSailYAMLGenerator     *gen.YAMLGenerator[ksailcluster.Cluster]
	KindGenerator          *gen.KindGenerator
	K3dGenerator           *gen.K3dGenerator
	KustomizationGenerator *gen.KustomizationGenerator
}

func (s *Scaffolder) Scaffold(output string, force bool) error {
	// generate ksail.yaml file
	_, err := s.KSailYAMLGenerator.Generate(s.KSailConfig, gen.Options{Output: output + "ksail.yaml", Force: force})
	if err != nil {
		return err
	}

	// generate distribution config file
	switch s.KSailConfig.Spec.Distribution {
	case ksailcluster.DistributionKind:
		if _, err := s.KindGenerator.Generate(gen.Options{Output: output + "kind.yaml", Force: force}); err != nil {
			return err
		}
	case ksailcluster.DistributionK3d:
		if _, err := s.K3dGenerator.Generate(gen.Options{Output: output + "k3d.yaml", Force: force}); err != nil {
			return err
		}
	case ksailcluster.DistributionTind:
		return fmt.Errorf("talos-in-docker distribution is not yet implemented")
	default:
		return fmt.Errorf("provided distribution is unknown")
	}

	if _, err := s.KustomizationGenerator.Generate(gen.Options{Output: filepath.Join(output, s.KSailConfig.Spec.SourceDirectory), Force: force}); err != nil {
		return err
	}

	return nil
}

func NewScaffolder(cfg ksailcluster.Cluster) *Scaffolder {
	ksailGen := gen.NewYAMLGenerator[ksailcluster.Cluster]()
	kindGen := gen.NewKindGenerator(&cfg)
	k3dGen := gen.NewK3dGenerator(&cfg)
	kustGen := gen.NewKustomizationGenerator(&cfg)

	return &Scaffolder{
		KSailConfig:            cfg,
		KSailYAMLGenerator:     ksailGen,
		KindGenerator:          kindGen,
		K3dGenerator:           k3dGen,
		KustomizationGenerator: kustGen,
	}
}

// --- internals ---
