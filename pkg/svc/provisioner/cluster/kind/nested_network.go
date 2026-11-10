package kindprovisioner

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/system"
	dockerclient "github.com/docker/docker/client"
)

// nestedKindNetworkName is the Docker network Kind attaches its nodes to.
const nestedKindNetworkName = "kind"

// ErrNestedKindNetworkInUse is returned when a persisted nested `kind` network predates the
// planned address pool but still has containers attached, so it cannot be replaced safely.
var ErrNestedKindNetworkInUse = errors.New("nested kind network predates the planned address pool")

// nestedNetworkClient is the part of the Docker API the nested network check uses.
type nestedNetworkClient interface {
	Info(ctx context.Context) (system.Info, error)
	NetworkInspect(
		ctx context.Context,
		networkID string,
		options network.InspectOptions,
	) (network.Inspect, error)
	NetworkRemove(ctx context.Context, networkID string) error
}

// reconcileNestedKindNetwork replaces a nested `kind` network that persisted DinD state kept from
// before the daemon's address pool was planned. Docker applies a new default pool only to networks
// it creates later, so such a network can keep a subnet that overlaps the host. An unused network
// outside the pool is removed so Kind recreates it from the pool; one with containers attached is
// refused, because removing it would cut those containers off.
func reconcileNestedKindNetwork(ctx context.Context, client nestedNetworkClient) error {
	info, err := client.Info(ctx)
	if err != nil {
		return fmt.Errorf("read nested Docker address pools: %w", err)
	}

	pools, err := ipv4AddressPools(info.DefaultAddressPools)
	if err != nil || len(pools) == 0 {
		return err
	}

	existing, err := client.NetworkInspect(ctx, nestedKindNetworkName, network.InspectOptions{})
	if cerrdefs.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("inspect nested kind network: %w", err)
	}

	outside, err := hasSubnetOutsidePools(existing.IPAM.Config, pools)
	if err != nil || !outside {
		return err
	}

	if len(existing.Containers) > 0 {
		return fmt.Errorf(
			"%w: its subnet is outside the planned pool and %d containers are still attached; "+
				"delete the cluster's persisted DinD state and retry",
			ErrNestedKindNetworkInUse, len(existing.Containers),
		)
	}

	err = client.NetworkRemove(ctx, existing.ID)
	if err != nil {
		return fmt.Errorf("remove nested kind network outside the planned pool: %w", err)
	}

	return nil
}

// ipv4AddressPools parses the daemon's IPv4 default address pool bases.
func ipv4AddressPools(pools []system.NetworkAddressPool) ([]netip.Prefix, error) {
	result := make([]netip.Prefix, 0, len(pools))

	for _, pool := range pools {
		prefix, err := netip.ParsePrefix(pool.Base)
		if err != nil {
			return nil, fmt.Errorf("parse nested Docker address pool: %w", err)
		}

		if prefix.Addr().Is4() {
			result = append(result, prefix.Masked())
		}
	}

	return result, nil
}

// hasSubnetOutsidePools reports whether any IPv4 subnet of the network lies outside every pool.
// IPv6 subnets are ignored: the pool only steers IPv4 allocation.
func hasSubnetOutsidePools(configs []network.IPAMConfig, pools []netip.Prefix) (bool, error) {
	for _, config := range configs {
		if config.Subnet == "" {
			continue
		}

		subnet, err := netip.ParsePrefix(config.Subnet)
		if err != nil {
			return false, fmt.Errorf("parse nested kind network subnet: %w", err)
		}

		if !subnet.Addr().Is4() {
			continue
		}

		inside := slices.ContainsFunc(pools, func(pool netip.Prefix) bool {
			return pool.Bits() <= subnet.Bits() && pool.Contains(subnet.Addr())
		})
		if !inside {
			return true, nil
		}
	}

	return false, nil
}

// replaceStaleNestedKindNetwork runs reconcileNestedKindNetwork against the nested daemon that
// DOCKER_HOST points at.
func replaceStaleNestedKindNetwork(ctx context.Context) error {
	client, err := dockerclient.NewClientWithOpts(
		dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return fmt.Errorf("create nested Docker client: %w", err)
	}

	defer func() { _ = client.Close() }()

	return reconcileNestedKindNetwork(ctx, client)
}
