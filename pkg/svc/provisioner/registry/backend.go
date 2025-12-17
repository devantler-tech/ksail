package registry

import (
	"context"

	dockerclient "github.com/devantler-tech/ksail-go/pkg/client/docker"
)

// Backend defines the minimal registry operations required by both mirror and local registry flows.
type Backend interface {
	CreateRegistry(ctx context.Context, config dockerclient.RegistryConfig) error
	DeleteRegistry(
		ctx context.Context,
		name, clusterName string,
		deleteVolume bool,
		networkName string,
		volumeName string,
	) error
	ListRegistries(ctx context.Context) ([]string, error)
	GetRegistryPort(ctx context.Context, name string) (int, error)
}
