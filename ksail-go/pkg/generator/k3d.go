package generator

import (
	"fmt"

	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
	"github.com/devantler-tech/ksail/pkg/io"
	"github.com/devantler-tech/ksail/pkg/marshaller"
	"github.com/k3d-io/k3d/v5/pkg/config/types"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
)

// K3dGenerator generates a k3d SimpleConfig YAML.
type K3dGenerator struct {
	io.FileWriter
	Cluster    *ksailcluster.Cluster
	Marshaller marshaller.Marshaller[*v1alpha5.SimpleConfig]
}

func (g *K3dGenerator) Generate(opts Options) (string, error) {
	cfg := v1alpha5.SimpleConfig{
		TypeMeta: types.TypeMeta{APIVersion: "k3d.io/v1alpha5", Kind: "Simple"},
		ObjectMeta: types.ObjectMeta{
			Name: g.Cluster.Metadata.Name,
		},
	}

	out, err := g.Marshaller.Marshal(&cfg)
	if err != nil {
		return "", fmt.Errorf("marshal k3d config: %w", err)
	}
	return g.FileWriter.TryWrite(out, opts.Output, opts.Force)
}

func NewK3dGenerator(cfg *ksailcluster.Cluster) *K3dGenerator {
	return &K3dGenerator{
		Cluster:    cfg,
		Marshaller: marshaller.NewMarshaller[*v1alpha5.SimpleConfig](),
	}
}
