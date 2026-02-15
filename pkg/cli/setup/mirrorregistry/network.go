package mirrorregistry

import (
	"context"
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// DefaultNetworkMTU is the default MTU for Docker bridge networks.
// Required by the Talos SDK's Reflect() function which reads
// com.docker.network.driver.mtu to parse network state.
const DefaultNetworkMTU = "1500"

// EnsureDockerNetworkExists creates a Docker network if it doesn't already exist.
// This is used to pre-create the cluster network before registry setup,
// allowing registry containers to be connected and accessible via Docker DNS when
// nodes start pulling images during boot.
//
// The network is created with Talos-compatible labels and CIDR so that the Talos SDK
// will recognize and reuse it when creating the cluster.
func EnsureDockerNetworkExists(
	ctx context.Context,
	dockerClient client.APIClient,
	networkName string,
	networkCIDR string,
	writer io.Writer,
) error {
	exists, err := networkExists(ctx, dockerClient, networkName, writer)
	if err != nil {
		return err
	}

	if exists {
		return nil
	}

	return createDockerNetwork(ctx, dockerClient, networkName, networkCIDR, writer)
}

// networkExists checks if a Docker network with the given name already exists.
func networkExists(
	ctx context.Context,
	dockerClient client.APIClient,
	networkName string,
	writer io.Writer,
) (bool, error) {
	networks, err := dockerClient.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", networkName)),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list networks: %w", err)
	}

	for _, nw := range networks {
		if nw.Name == networkName {
			notify.WriteMessage(notify.Message{
				Type:    notify.ActivityType,
				Content: "network '%s' already exists",
				Writer:  writer,
				Args:    []any{networkName},
			})

			return true, nil
		}
	}

	return false, nil
}

// createDockerNetwork creates a Docker network with Talos-compatible configuration.
func createDockerNetwork(
	ctx context.Context,
	dockerClient client.APIClient,
	networkName string,
	networkCIDR string,
	writer io.Writer,
) error {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "creating network '%s'",
		Writer:  writer,
		Args:    []any{networkName},
	})

	createOptions := buildNetworkCreateOptions(networkName, networkCIDR)

	_, err := dockerClient.NetworkCreate(ctx, networkName, createOptions)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	return nil
}

// buildNetworkCreateOptions constructs the Docker network creation configuration
// with Talos-compatible labels, CIDR, and bridge options.
func buildNetworkCreateOptions(networkName, networkCIDR string) network.CreateOptions {
	createOptions := network.CreateOptions{
		Driver: "bridge",
		// Use Talos labels so the SDK recognizes this as a Talos network
		Labels: map[string]string{
			"talos.owned":        "true",
			"talos.cluster.name": networkName,
		},
		// Enable container name DNS resolution and set MTU
		// The MTU option is required by the Talos SDK's Reflect() function which
		// reads com.docker.network.driver.mtu to parse network state. Without it,
		// strconv.Atoi("") fails with "invalid syntax" during cluster deletion.
		Options: map[string]string{
			"com.docker.network.bridge.enable_icc":           "true",
			"com.docker.network.bridge.enable_ip_masquerade": "true",
			"com.docker.network.driver.mtu":                  DefaultNetworkMTU,
		},
	}

	// Add IPAM config if CIDR is provided
	if networkCIDR != "" {
		createOptions.IPAM = &network.IPAM{
			Config: []network.IPAMConfig{
				{
					Subnet: networkCIDR,
				},
			},
		}
	}

	return createOptions
}
