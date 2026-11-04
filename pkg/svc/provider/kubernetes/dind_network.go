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

	excluded = append(excluded, p.hostServiceAddresses(ctx)...)

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

// hostServiceAddresses returns the ClusterIPs of the host's API and DNS Services. The DinD pod
// resolves through the host DNS ClusterIP (dnsPolicy ClusterFirst), and a nested bridge whose
// /24 contains that address would capture the pod's DNS queries. Node objects do not carry the
// Service range, so exclude the /16 around these well-known addresses instead. This is
// best-effort: a missing or unreadable Service only means no additional exclusion, never a
// weaker one than the node inventory already established.
func (p *Provider) hostServiceAddresses(ctx context.Context) []netip.Prefix {
	var networks []netip.Prefix

	for _, ref := range []struct{ namespace, name string }{
		{metav1.NamespaceDefault, "kubernetes"},
		{metav1.NamespaceSystem, "kube-dns"},
	} {
		svc, err := p.client.CoreV1().
			Services(ref.namespace).
			Get(ctx, ref.name, metav1.GetOptions{})
		if err != nil {
			continue
		}

		ips := slices.Clone(svc.Spec.ClusterIPs)
		if svc.Spec.ClusterIP != "" {
			ips = append(ips, svc.Spec.ClusterIP)
		}

		for _, ip := range ips {
			addr, parseErr := netip.ParseAddr(ip)
			if parseErr != nil {
				continue // "None" for headless Services.
			}

			addr = addr.Unmap()
			networks = append(networks, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}

	return networks
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
