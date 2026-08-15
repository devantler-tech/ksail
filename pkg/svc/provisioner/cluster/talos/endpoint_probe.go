package talosprovisioner

import (
	"context"
	"fmt"
	"time"
)

const (
	// kubernetesAPIPort is the port the Kubernetes API server listens on for
	// every KSail-managed Talos cluster.
	kubernetesAPIPort = 6443

	// secretsSyncEndpointProbeTimeout bounds the reachability probe of the
	// endpoint read from a running cluster's config during secret sync. A live
	// endpoint answers within one dial round-trip; the window only has to
	// absorb transient network jitter, and a short window keeps updates from
	// stalling when the recorded endpoint is dead (a floating IP that was
	// attached but never claimed on the node — ksail#6070).
	secretsSyncEndpointProbeTimeout = 10 * time.Second

	// floatingIPEndpointProbeTimeout bounds the reachability probe of a
	// freshly reconciled floating IP before it is persisted as the kubeconfig
	// endpoint. The Talos hcloud VIP controller claims the address
	// asynchronously after the machine config lands, so this window allows
	// the claim to settle without letting a never-claimed IP stall the update
	// indefinitely.
	floatingIPEndpointProbeTimeout = 30 * time.Second
)

// verifiedEndpointIP returns candidateIP when the Kubernetes API accepts TCP
// connections on candidateIP:6443 within timeout, and fallbackIP (with a
// logged warning) otherwise. A Hetzner floating IP is attached via the cloud
// API but only becomes reachable once the node claims it, so persisting an
// unverified endpoint into cluster secrets or a kubeconfig can record an
// address nothing answers on (ksail#6070). fallbackIP must be an address that
// is already known reachable (a control-plane node IP kept in the cert SANs).
func (p *Provisioner) verifiedEndpointIP(
	ctx context.Context,
	candidateIP, fallbackIP string,
	timeout time.Duration,
) string {
	if candidateIP == fallbackIP {
		return candidateIP
	}

	err := p.apiEndpointReachabilityCheck(ctx, candidateIP, timeout)
	if err == nil {
		return candidateIP
	}

	_, _ = fmt.Fprintf(
		p.logWriter,
		"  ⚠️ Cluster endpoint %s does not answer on port %d; using %s instead"+
			" (the floating IP may not be claimed on the node — see ksail#6070)\n",
		candidateIP,
		kubernetesAPIPort,
		fallbackIP,
	)

	return fallbackIP
}
