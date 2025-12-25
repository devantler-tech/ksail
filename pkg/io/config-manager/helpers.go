package configmanager

import (
	"errors"
	"fmt"

	talos "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

var errUnsupportedConfigType = errors.New("unsupported config type")

// TalosInDockerConfigNameProvider is an interface for types that can provide a cluster name.
// This is used to break the import cycle with the talosindocker provisioner package.
type TalosInDockerConfigNameProvider interface {
	GetClusterName() string
}

// GetClusterName extracts the cluster name from supported Kind, K3d, or Talos config structures.
func GetClusterName(config any) (string, error) {
	switch cfg := config.(type) {
	case *v1alpha4.Cluster:
		return cfg.Name, nil
	case *v1alpha5.SimpleConfig:
		return cfg.Name, nil
	case *talos.Configs:
		return cfg.Name, nil
	case TalosInDockerConfigNameProvider:
		return cfg.GetClusterName(), nil
	default:
		return "", fmt.Errorf("%w: %T", errUnsupportedConfigType, cfg)
	}
}
