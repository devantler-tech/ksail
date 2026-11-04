package kubernetes

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	dindNodePageSize  = 500
	dindInventoryWait = 30 * time.Second
	// kindIPv6Prefix holds every IPv6 subnet Kind assigns its Docker network.
	kindIPv6Prefix = "fc00::/16"
)

// A nested daemon cannot see the host nodes' connected routes. Docker can therefore
// allocate the same bridge subnet on both sides of the exposed API, sending replies
// into the inner bridge instead of back to the host. Configure its automatic IPv4
// pools before it starts; leave Kind's normal network creation and IPv6 behavior intact, and
// refuse a host whose networks already use Kind's IPv6 range, which no daemon pool can avoid.
// The node inventory is a guard on the allow-list, not the selection itself: it fails
// creation when a host node already uses a candidate rather than guessing another range.
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

	// Kind fixes the first two bytes of its Docker network's IPv6 /64 to fc00 and derives the
	// rest from the network name, so a nested `kind` network gets the same /64 as an outer one.
	// The address pool cannot steer that choice, so a host already using this range is refused
	// instead of reporting a guard that does not hold.
	kindIPv6Range := netip.MustParsePrefix(kindIPv6Prefix)
	if slices.ContainsFunc(excluded, kindIPv6Range.Overlaps) {
		return "", fmt.Errorf(
			"host network uses Kind's IPv6 range %s, which the nested Kind network would reuse: %w",
			kindIPv6Range, ErrDinDAddressPool,
		)
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

// selectDinDPool chooses the nested Docker pool from an allow-list: the two /16s of
// 198.18.0.0/15, which RFC 2544 reserves for network benchmarking. Every host range a Kubernetes
// cluster commonly routes (node networks, pod ranges, Service ranges, CNI IPAM pools and Docker's
// own defaults) is drawn from RFC 1918 space by convention, and none of those can be enumerated
// completely from the Kubernetes API. Choosing from a range outside them avoids the whole class
// instead of excluding the ranges one inventory read happens to see.
func selectDinDPool(excluded []netip.Prefix) (string, error) {
	for _, candidate := range []netip.Prefix{
		netip.MustParsePrefix("198.18.0.0/16"),
		netip.MustParsePrefix("198.19.0.0/16"),
	} {
		if !slices.ContainsFunc(excluded, candidate.Overlaps) {
			return candidate.String(), nil
		}
	}

	return "", ErrDinDAddressPool
}

// verifyExistingDinDPool rejects a reused DinD pod whose daemon was not started with the planned
// address pool (for example one created before pools were planned). Container args are immutable
// on a running pod, so the only safe outcome for a mismatch is an error asking for its removal.
func (p *Provider) verifyExistingDinDPool(ctx context.Context, namespace, poolArg string) error {
	existing, err := p.client.CoreV1().Pods(namespace).Get(ctx, DinDPodName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read existing DinD pod: %w", err)
	}

	for _, container := range existing.Spec.Containers {
		if container.Name == DinDContainerName && slices.Contains(container.Args, poolArg) {
			return nil
		}
	}

	return fmt.Errorf(
		"existing DinD pod %s/%s does not use the planned nested Docker address pool; "+
			"delete the pod and retry: %w",
		namespace, DinDPodName, ErrDinDAddressPool,
	)
}

// plannedDinDPoolArg returns the daemon argument that pins Vanilla's nested Docker address pool,
// or "" for distributions that keep Docker's defaults.
func (p *Provider) plannedDinDPoolArg(ctx context.Context, distribution string) (string, error) {
	if distribution != string(v1alpha1.DistributionVanilla) {
		return "", nil
	}

	pool, err := p.kindDinDAddressPool(ctx)
	if err != nil {
		return "", fmt.Errorf("plan nested Docker network: %w", err)
	}

	return "--default-address-pool=base=" + pool + ",size=24", nil
}

// createOrReuseDinDPod creates the DinD pod. A reused daemon keeps the pools it started with, so
// when a pool is planned an existing pod must already carry it.
func (p *Provider) createOrReuseDinDPod(
	ctx context.Context,
	namespace string,
	pod *corev1.Pod,
	poolArg string,
) error {
	_, err := p.client.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err == nil {
		return nil
	}

	if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create DinD pod: %w", err)
	}

	if poolArg == "" {
		return nil
	}

	return p.verifyExistingDinDPool(ctx, namespace, poolArg)
}
