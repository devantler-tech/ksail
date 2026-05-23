package mirrorregistry

import (
	"context"
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
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
	dockerClient client.APIClient,
	networkName string,
	networkCIDR string,
	writer io.Writer,
) error {
	err := registry.EnsureNetwork(ctx, dockerClient, networkName, networkCIDR, writer)
	if err != nil {
		return fmt.Errorf("ensure docker network: %w", err)
	}

	return nil
}
