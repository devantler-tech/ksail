package mirrorregistry

import (
	"context"
	"fmt"
	"io"

	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
)

// DefaultNetworkMTU is the default MTU for Docker bridge networks.
// Required by the Talos SDK's Reflect() function which reads
// com.docker.network.driver.mtu to parse network state.
const DefaultNetworkMTU = registry.DefaultNetworkMTU

// EnsureDockerNetworkExists creates a Docker network if it doesn't already exist.
// It delegates to registry.EnsureNetwork (shared with the nested Kubernetes-provider
// mirror setup) so the cluster network can be pre-created with Talos-compatible labels
// and CIDR before registry containers are connected.
func EnsureDockerNetworkExists(
	ctx context.Context,
	dockerClient dockerclient.Client,
	networkName string,
	networkCIDR string,
	writer io.Writer,
) error {
	// Host (non-DinD) networks use the standard MTU.
	err := registry.EnsureNetwork(
		ctx, dockerClient, networkName, networkCIDR, registry.DefaultNetworkMTU, writer,
	)
	if err != nil {
		return fmt.Errorf("ensure docker network: %w", err)
	}

	return nil
}
