package mirrorregistry

import (
	"context"
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
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
//
//nolint:funlen // Network creation requires multiple configuration steps
func EnsureDockerNetworkExists(
	ctx context.Context,
	dockerClient client.APIClient,
	networkName string,
	networkCIDR string,
	writer io.Writer,
) error {
	// Check if network already exists
	networks, err := dockerClient.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", networkName)),
	})
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}

	// Network with exact name match
	for _, nw := range networks {
		if nw.Name == networkName {
			notify.WriteMessage(notify.Message{
				Type:    notify.ActivityType,
				Content: "network '%s' already exists",
				Writer:  writer,
				Args:    []any{networkName},
			})

			return nil
		}
	}

	// Create the network with Talos-compatible labels and CIDR
	// This ensures the Talos SDK will recognize and reuse the network
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "creating network '%s'",
		Writer:  writer,
		Args:    []any{networkName},
	})

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

	_, err = dockerClient.NetworkCreate(ctx, networkName, createOptions)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	return nil
}
