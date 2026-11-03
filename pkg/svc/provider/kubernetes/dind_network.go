package kubernetes

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	dindPoolPrefixBits = 16
	dindNodePageSize   = 500
	dindInventoryWait  = 30 * time.Second
)

// A nested daemon cannot see the host nodes' connected routes. Docker can therefore
// allocate the same bridge subnet on both sides of the exposed API, sending replies
// into the inner bridge instead of back to the host. Configure its automatic IPv4
// pools before it starts; leave Kind's normal network creation and IPv6 behavior intact.
func (p *Provider) kindDinDAddressPool(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, dindInventoryWait)
	defer cancel()

	var excluded []netip.Prefix

	seen := make(map[string]bool)
	continuation := ""

	for {
		if seen[continuation] {
			return "", fmt.Errorf("repeated host inventory page: %w", ErrDinDAddressPool)
		}

		seen[continuation] = true

		page, err := p.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{
			Limit: dindNodePageSize, Continue: continuation,
		})
		if err != nil {
			return "", fmt.Errorf("read host network inventory: %w", err)
		}

		for idx := range page.Items {
			networks, err := hostNodeNetworks(&page.Items[idx])
			if err != nil {
				return "", err
			}

			excluded = append(excluded, networks...)
		}

		continuation = page.Continue
		if continuation == "" {
			break
		}
	}

	if len(excluded) == 0 {
		return "", fmt.Errorf("empty host network inventory: %w", ErrDinDAddressPool)
	}

	return selectDinDPool(excluded)
}

func hostNodeNetworks(node *corev1.Node) ([]netip.Prefix, error) {
	networks, err := hostNodeAddresses(node)
	if err != nil {
		return nil, err
	}

	cidrs := slices.Clone(node.Spec.PodCIDRs)
	if node.Spec.PodCIDR != "" {
		cidrs = append(cidrs, node.Spec.PodCIDR)
	}

	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil || prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("invalid host pod range: %w", ErrDinDAddressPool)
		}

		networks = append(networks, prefix.Masked())
	}

	return networks, nil
}

func hostNodeAddresses(node *corev1.Node) ([]netip.Prefix, error) {
	var networks []netip.Prefix

	for _, address := range node.Status.Addresses {
		if address.Type != corev1.NodeInternalIP && address.Type != corev1.NodeExternalIP {
			continue
		}

		nodeAddress, err := netip.ParseAddr(address.Address)
		if err != nil || nodeAddress.Zone() != "" {
			return nil, fmt.Errorf("invalid host node address: %w", ErrDinDAddressPool)
		}

		nodeAddress = nodeAddress.Unmap()
		networks = append(networks, netip.PrefixFrom(nodeAddress, nodeAddress.BitLen()))
	}

	if len(networks) == 0 {
		return nil, fmt.Errorf("missing host node address: %w", ErrDinDAddressPool)
	}

	return networks, nil
}

func selectDinDPool(excluded []netip.Prefix) (string, error) {
	for _, block := range []netip.Prefix{
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
		netip.MustParsePrefix("10.0.0.0/8"),
	} {
		for offset := range 1 << (dindPoolPrefixBits - block.Bits()) {
			address := block.Addr().As4()
			address[1] += byte(offset)
			candidate := netip.PrefixFrom(netip.AddrFrom4(address), dindPoolPrefixBits)

			if !slices.ContainsFunc(excluded, candidate.Overlaps) {
				return candidate.String(), nil
			}
		}
	}

	return "", ErrDinDAddressPool
}
