package registry

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// ConnectRegistriesToNetwork attaches each registry container to the provided network.
// Any connection failures are logged as warnings but do not abort the operation.
func ConnectRegistriesToNetwork(
	ctx context.Context,
	dockerClient client.APIClient,
	registries []Info,
	networkName string,
	writer io.Writer,
) error {
	networkName, networkOK := TrimNonEmpty(networkName)
	if dockerClient == nil || len(registries) == 0 || !networkOK {
		return nil
	}

	for _, reg := range registries {
		connectRegistryToNetwork(ctx, dockerClient, reg, networkName, "", writer)
	}

	return nil
}

// ConnectRegistriesToNetworkWithStaticIPs attaches registry containers to a network using static IPs
// from the high end of the subnet to avoid conflicts with Talos node IPs that start from .2.
// For a /24 network, registries are assigned starting from .250 down (.250, .249, .248, etc.).
// Returns a map of registry names to their assigned static IPs.
func ConnectRegistriesToNetworkWithStaticIPs(
	ctx context.Context,
	dockerClient client.APIClient,
	registries []Info,
	networkName string,
	networkCIDR string,
	writer io.Writer,
) (map[string]string, error) {
	networkName, networkOK := TrimNonEmpty(networkName)
	if dockerClient == nil || len(registries) == 0 || !networkOK {
		return make(map[string]string), nil
	}

	// Calculate static IPs for each registry from the high end of the subnet
	// For 10.5.0.0/24: use 10.5.0.250, 10.5.0.249, etc.
	staticIPs := calculateRegistryIPs(networkCIDR, len(registries))
	registryIPs := make(map[string]string, len(registries))

	for regIdx, reg := range registries {
		registryIP := connectRegistryToNetwork(
			ctx,
			dockerClient,
			reg,
			networkName,
			staticIPAt(staticIPs, regIdx),
			writer,
		)
		if registryIP != "" {
			containerName, _ := TrimNonEmpty(reg.Name)
			registryIPs[containerName] = registryIP
		}
	}

	return registryIPs, nil
}

// connectRegistryToNetwork connects a single registry to a network, optionally with a static IP.
// Returns the assigned static IP (empty if none was assigned or the connection failed).
func connectRegistryToNetwork(
	ctx context.Context,
	dockerClient client.APIClient,
	reg Info,
	networkName string,
	staticIP string,
	writer io.Writer,
) string {
	containerName, nameOK := TrimNonEmpty(reg.Name)
	if !nameOK {
		return ""
	}

	var endpointConfig *network.EndpointSettings

	if staticIP != "" {
		endpointConfig = &network.EndpointSettings{
			IPAMConfig: &network.EndpointIPAMConfig{
				IPv4Address: staticIP,
			},
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "connecting '%s' to '%s' with IP %s",
			Writer:  writer,
			Args:    []any{containerName, networkName, staticIP},
		})
	} else {
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "connecting '%s' to '%s'",
			Writer:  writer,
			Args:    []any{containerName, networkName},
		})
	}

	err := dockerClient.NetworkConnect(ctx, networkName, containerName, endpointConfig)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type: notify.ErrorType,
			Content: fmt.Sprintf(
				"failed to connect registry %s to %s network: %v",
				containerName,
				networkName,
				err,
			),
			Writer: writer,
		})

		return ""
	}

	return staticIP
}

// staticIPAt returns the static IP at the given index, or empty if out of bounds.
func staticIPAt(ips []string, idx int) string {
	if idx < len(ips) {
		return ips[idx]
	}

	return ""
}

// calculateRegistryIPs computes static IPs for registries from the high end of the subnet.
// For a /24 network like 10.5.0.0/24, it returns addresses like 10.5.0.250, 10.5.0.249, etc.
// Returns empty strings if the CIDR is invalid or cannot be parsed.
func calculateRegistryIPs(networkCIDR string, count int) []string {
	const baseOffset = 250

	result := make([]string, count)

	if networkCIDR == "" || count == 0 {
		return result
	}

	_, ipNet, err := net.ParseCIDR(networkCIDR)
	if err != nil {
		return result
	}

	ipv4 := ipNet.IP.To4()
	if ipv4 == nil {
		return result
	}

	// For a /24 network, assign from .250 down to avoid node IPs starting at .2
	// This gives space for ~248 nodes before we'd have a conflict
	for i := 0; i < count && i < baseOffset-2; i++ {
		addr := make(net.IP, len(ipv4))
		copy(addr, ipv4)
		addr[3] = byte(baseOffset - i)
		result[i] = addr.String()
	}

	return result
}

// SetupMirrorSpecRegistries prepares a registry manager from mirror specifications
// and ensures that the matching registry containers exist. This is a convenience function
// that combines PrepareRegistryManagerFromSpecs with SetupRegistries.
func SetupMirrorSpecRegistries(
	ctx context.Context,
	mirrorSpecs []MirrorSpec,
	clusterName string,
	dockerClient client.APIClient,
	networkName string,
	writer io.Writer,
) error {
	registryMgr, registriesInfo, err := PrepareRegistryManagerFromSpecs(
		ctx, mirrorSpecs, clusterName, dockerClient,
	)
	if err != nil {
		return err
	}

	if registryMgr == nil {
		return nil
	}

	return SetupRegistries(
		ctx, registryMgr, registriesInfo, clusterName, networkName, writer,
	)
}

// CleanupMirrorSpecRegistries prepares a registry manager from mirror specifications
// and removes the matching registry containers. This is a convenience function that
// combines PrepareRegistryManagerFromSpecs with CleanupRegistries.
func CleanupMirrorSpecRegistries(
	ctx context.Context,
	mirrorSpecs []MirrorSpec,
	clusterName string,
	dockerClient client.APIClient,
	deleteVolumes bool,
	networkName string,
) error {
	registryMgr, registriesInfo, err := PrepareRegistryManagerFromSpecs(
		ctx, mirrorSpecs, clusterName, dockerClient,
	)
	if err != nil {
		return err
	}

	if registryMgr == nil {
		return nil
	}

	return CleanupRegistries(
		ctx, registryMgr, registriesInfo, clusterName, deleteVolumes, networkName, nil,
	)
}

// ConnectMirrorSpecsToNetwork builds registry infos from mirror specifications and connects
// them to the specified Docker network. This is a convenience function that combines
// BuildRegistryInfosFromSpecs with ConnectRegistriesToNetwork.
func ConnectMirrorSpecsToNetwork(
	ctx context.Context,
	mirrorSpecs []MirrorSpec,
	clusterName string,
	networkName string,
	dockerClient client.APIClient,
	writer io.Writer,
) error {
	if len(mirrorSpecs) == 0 {
		return nil
	}

	registriesInfo := BuildRegistryInfosFromSpecs(mirrorSpecs, nil, nil, clusterName)
	if len(registriesInfo) == 0 {
		return nil
	}

	return ConnectRegistriesToNetwork(ctx, dockerClient, registriesInfo, networkName, writer)
}

// CleanupRegistries removes the provided registry. Errors are logged as warnings.
func CleanupRegistries(
	ctx context.Context,
	registryMgr Backend,
	registries []Info,
	clusterName string,
	deleteVolumes bool,
	networkName string,
	warningWriter io.Writer,
) error {
	if registryMgr == nil || len(registries) == 0 {
		return nil
	}

	writer := warningWriter
	if writer == nil {
		writer = os.Stderr
	}

	for _, reg := range registries {
		err := registryMgr.DeleteRegistry(
			ctx,
			reg.Name,
			clusterName,
			deleteVolumes,
			networkName,
			reg.Volume,
		)
		if err != nil {
			_, _ = fmt.Fprintf(
				writer,
				"Warning: failed to cleanup registry %s: %v\n",
				reg.Name,
				err,
			)
		}
	}

	return nil
}
