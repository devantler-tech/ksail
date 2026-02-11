package configmanager

import (
	"errors"
	"fmt"

	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

var errUnsupportedConfigType = errors.New("unsupported config type")

// ClusterNameProvider is an interface for types that can provide a cluster name.
// This allows GetClusterName to work with any config type that implements this method,
// including talos.Configs and TalosConfig.
type ClusterNameProvider interface {
	GetClusterName() string
}

// GetClusterName extracts the cluster name from supported Kind, K3d, or Talos config structures.
// For Talos and Talos configs, use types implementing ClusterNameProvider.
func GetClusterName(config any) (string, error) {
	switch cfg := config.(type) {
	case *v1alpha4.Cluster:
		return cfg.Name, nil
	case *v1alpha5.SimpleConfig:
		return cfg.Name, nil
	case ClusterNameProvider:
		return cfg.GetClusterName(), nil
	default:
		return "", fmt.Errorf("%w: %T", errUnsupportedConfigType, cfg)
	}
}
