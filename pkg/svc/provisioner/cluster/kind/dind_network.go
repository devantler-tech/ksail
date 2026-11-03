package kindprovisioner

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	"github.com/docker/docker/api/types/network"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

type dindNetworkPlan struct {
	pool      string
	base      netip.Prefix
	protected []netip.Prefix
}

// planDinDNetwork protects known required addresses, not unknown enclosing
// networks. The policy tries RFC1918 /12 blocks in address order, retaining
// sixteen /16 networks per daemon (including docker0 and Kind). The smaller
// RFC1918 /16 block cannot provide that capacity and is not a candidate.
// Docker additionally excludes its own DNS servers and on-link pod routes.
// There is no fallback outside the selected pool or retry with another pool.
func planDinDNetwork(
	ctx context.Context, cfg *v1alpha4.Cluster, knownNetworks []string, endpoints ...string,
) (dindNetworkPlan, error) {
	protected := make([]netip.Prefix, 0)

	for _, endpoint := range endpoints {
		prefixes, err := endpointPrefixes(ctx, endpoint, net.DefaultResolver.LookupNetIP)
		if err != nil {
			return dindNetworkPlan{}, err
		}

		protected = append(protected, prefixes...)
	}

	// Default a networking-only copy so upstream defaults, including dual stack,
	// match Kind without modifying the caller's nodes or API bind address.
	effective := &v1alpha4.Cluster{Networking: cfg.Networking}
	v1alpha4.SetDefaultsCluster(effective)

	subnetLists := append(
		[]string{effective.Networking.PodSubnet, effective.Networking.ServiceSubnet},
		knownNetworks...)
	for _, subnets := range subnetLists {
		for subnet := range strings.SplitSeq(subnets, ",") {
			subnet = strings.TrimSpace(subnet)

			prefix, err := netip.ParsePrefix(subnet)

			address, addressErr := netip.ParseAddr(subnet)
			if addressErr == nil {
				address = address.Unmap()
				prefix, err = netip.PrefixFrom(address, address.BitLen()), nil
			}

			if err != nil {
				return dindNetworkPlan{}, fmt.Errorf("parse Kind network: %w", err)
			}

			protected = append(protected, prefix.Masked())
		}
	}

	return selectDinDNetwork(protected)
}

func selectDinDNetwork(protected []netip.Prefix) (dindNetworkPlan, error) {
	// Sixteen /12 blocks in 10/8, followed by the RFC1918 172.16/12 block.
	for _, candidate := range []string{
		"10.0.0.0/12", "10.16.0.0/12", "10.32.0.0/12", "10.48.0.0/12",
		"10.64.0.0/12", "10.80.0.0/12", "10.96.0.0/12", "10.112.0.0/12",
		"10.128.0.0/12", "10.144.0.0/12", "10.160.0.0/12", "10.176.0.0/12",
		"10.192.0.0/12", "10.208.0.0/12", "10.224.0.0/12", "10.240.0.0/12",
		"172.16.0.0/12",
	} {
		base := netip.MustParsePrefix(candidate)

		if !slices.ContainsFunc(protected, base.Overlaps) {
			return dindNetworkPlan{
				pool: "base=" + base.String() + ",size=16", base: base, protected: protected,
			}, nil
		}
	}

	return dindNetworkPlan{}, ErrNoDinDAddressPool
}

func endpointPrefixes(
	ctx context.Context, endpoint string,
	lookup func(context.Context, string, string) ([]netip.Addr, error),
) ([]netip.Prefix, error) {
	host, err := endpointHostname(endpoint)
	if err != nil {
		return nil, err
	}

	if host == "" {
		return nil, ErrUnresolvedDinDEndpoint
	}

	addresses := make([]netip.Addr, 0, 1)

	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err == nil {
		addresses = append(addresses, address)
	} else {
		var lookupErr error

		addresses, lookupErr = lookup(ctx, "ip", host)
		if lookupErr != nil {
			return nil, fmt.Errorf("resolve protected endpoint: %w", lookupErr)
		}
	}

	if len(addresses) == 0 {
		return nil, ErrUnresolvedDinDEndpoint
	}

	prefixes := make([]netip.Prefix, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !address.IsValid() || address.IsUnspecified() {
			return nil, ErrUnresolvedDinDEndpoint
		}

		prefixes = append(prefixes, netip.PrefixFrom(address, address.BitLen()))
	}

	return prefixes, nil
}

func endpointHostname(endpoint string) (string, error) {
	if strings.Contains(endpoint, "://") {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return "", fmt.Errorf("parse protected endpoint: %w", err)
		}

		return parsed.Hostname(), nil
	}

	host, _, err := net.SplitHostPort(endpoint)
	if err == nil {
		return host, nil
	}

	return endpoint, nil
}

type networkLister interface {
	NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error)
}

// Startup arguments do not migrate a persisted network. Validate actual bridge
// allocations both before Kind creates nodes and before publishing success.
func validateDinDNetworks(
	ctx context.Context, client networkLister, plan dindNetworkPlan, requireKind bool,
) error {
	networks, err := client.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return fmt.Errorf("inspect nested Docker networks: %w", err)
	}

	counts := make(map[string]int, len(networks))
	for _, existing := range networks {
		counts[existing.Name]++
		if counts[existing.Name] > 1 && requiredDinDNetwork(existing.Name) {
			return ErrIncompatibleDinDNetwork
		}

		if existing.Driver != network.NetworkBridge {
			if requiredDinDNetwork(existing.Name) {
				return ErrIncompatibleDinDNetwork
			}

			continue
		}

		err = validateDinDBridge(existing, plan)
		if err != nil {
			return err
		}
	}

	if !completeDinDNetworks(counts, requireKind) {
		return ErrIncompatibleDinDNetwork
	}

	return nil
}

func completeDinDNetworks(counts map[string]int, requireKind bool) bool {
	return counts[network.NetworkBridge] > 0 &&
		(!requireKind || counts[kindconfigmanager.DefaultNetworkName] > 0)
}

func requiredDinDNetwork(name string) bool {
	return name == network.NetworkBridge || name == kindconfigmanager.DefaultNetworkName
}

func validateDinDBridge(existing network.Summary, plan dindNetworkPlan) error {
	ipv4 := 0

	for _, allocation := range existing.IPAM.Config {
		prefix, parseErr := netip.ParsePrefix(allocation.Subnet)
		if parseErr != nil {
			return fmt.Errorf("inspect nested Docker subnet: %w", parseErr)
		}

		if !prefix.Addr().Is4() {
			continue
		}

		ipv4++

		if prefix.Bits() != 16 || !plan.base.Contains(prefix.Masked().Addr()) {
			return ErrIncompatibleDinDNetwork
		}

		if slices.ContainsFunc(plan.protected, prefix.Overlaps) {
			return ErrIncompatibleDinDNetwork
		}
	}

	if ipv4 != 1 {
		return ErrIncompatibleDinDNetwork
	}

	return nil
}
