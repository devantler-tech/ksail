package generator

import (
	"fmt"

	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
	"github.com/devantler-tech/ksail/pkg/io"
	"github.com/devantler-tech/ksail/pkg/marshaller"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// KindGenerator generates a kind Cluster YAML.
type KindGenerator struct {
	io.FileWriter
	Cluster    *ksailcluster.Cluster
	Marshaller marshaller.Marshaller[*v1alpha4.Cluster]
}

func (g *KindGenerator) Generate(opts Options) (string, error) {
	cfg := v1alpha4.Cluster{
		TypeMeta: v1alpha4.TypeMeta{APIVersion: "kind.x-k8s.io/v1alpha4", Kind: "Cluster"},
	}
	v1alpha4.SetDefaultsCluster(&cfg)
	out, err := g.Marshaller.Marshal(&cfg)
	if err != nil {
		return "", fmt.Errorf("marshal kind config: %w", err)
	}
	return g.FileWriter.TryWrite(out, opts.Output, opts.Force)
}

func NewKindGenerator(cfg *ksailcluster.Cluster) *KindGenerator {
	return &KindGenerator{
		Cluster:    cfg,
		Marshaller: marshaller.NewMarshaller[*v1alpha4.Cluster](),
	}
}
