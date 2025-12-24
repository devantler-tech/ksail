package configmanager

import (
	"errors"
	"fmt"

	talosindockerprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talosindocker"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

var errUnsupportedConfigType = errors.New("unsupported config type")

// GetClusterName extracts the cluster name from supported Kind, K3d, or TalosInDocker config structures.
func GetClusterName(config any) (string, error) {
	switch cfg := config.(type) {
	case *v1alpha4.Cluster:
		return cfg.Name, nil
	case *v1alpha5.SimpleConfig:
		return cfg.Name, nil
	case *talosindockerprovisioner.TalosInDockerConfig:
		return cfg.ClusterName, nil
	default:
		return "", fmt.Errorf("%w: %T", errUnsupportedConfigType, cfg)
	}
}
