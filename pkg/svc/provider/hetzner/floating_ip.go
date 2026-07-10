package hetzner

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// EnsureFloatingIP ensures an IPv4 floating IP exists for the cluster's
// control-plane endpoint, creating it if absent (get-by-name, create-if-absent,
// like EnsureNetwork/EnsureFirewall). The floating IP gives the Kubernetes API
// an address that survives any individual control-plane server being deleted
// and recreated; Talos reassigns it to the elected leader (VIP). homeLocation
// is the Hetzner location the IP is homed in (the cluster's configured
// location) — homing only affects routing latency, not which servers the IP
// can be assigned to.
func (p *Provider) EnsureFloatingIP(
	ctx context.Context,
	clusterName string,
	homeLocation string,
) (*hcloud.FloatingIP, error) {
	if p.client == nil {
		return nil, provider.ErrProviderUnavailable
	}

	floatingIPName := clusterName + FloatingIPSuffix

	// Check if the floating IP already exists
	floatingIP, _, err := p.client.FloatingIP.GetByName(ctx, floatingIPName)
	if err != nil {
		return nil, fmt.Errorf("failed to get floating IP %s: %w", floatingIPName, err)
	}

	if floatingIP != nil {
		// Only adopt a ksail-owned IP. A user-managed reserved address that
		// merely shares the conventional name must never be claimed for the
		// cluster (deleteFloatingIP applies the same ownership guard), and
		// creating a second IP under the same name would fail Hetzner's name
		// uniqueness anyway — so surface the collision instead.
		if floatingIP.Labels[LabelOwned] != LabelOwnedValue {
			return nil, fmt.Errorf(
				"%w: %s (release or rename it, or choose a different cluster name)",
				ErrFloatingIPNotOwned, floatingIPName,
			)
		}

		return floatingIP, nil
	}

	result, _, err := p.client.FloatingIP.Create(ctx, hcloud.FloatingIPCreateOpts{
		Type:         hcloud.FloatingIPTypeIPv4,
		Name:         new(floatingIPName),
		HomeLocation: &hcloud.Location{Name: homeLocation},
		Labels:       ResourceLabels(clusterName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create floating IP %s: %w", floatingIPName, err)
	}

	return result.FloatingIP, nil
}

// OwnedFloatingIPExists reports whether the cluster's ksail-owned floating IP
// currently exists — the read-only companion to EnsureFloatingIP, used by
// `cluster update` to diff the desired floatingIPEnabled state against what
// the cloud actually carries (#5947). It applies the same ownership guard: a
// floating IP that merely shares the conventional name but is not ksail-owned
// is surfaced as an error rather than counted as the cluster's.
func (p *Provider) OwnedFloatingIPExists(
	ctx context.Context,
	clusterName string,
) (bool, error) {
	if p.client == nil {
		return false, provider.ErrProviderUnavailable
	}

	floatingIPName := clusterName + FloatingIPSuffix

	floatingIP, _, err := p.client.FloatingIP.GetByName(ctx, floatingIPName)
	if err != nil {
		return false, fmt.Errorf("failed to get floating IP %s: %w", floatingIPName, err)
	}

	if floatingIP == nil {
		return false, nil
	}

	if floatingIP.Labels[LabelOwned] != LabelOwnedValue {
		return false, fmt.Errorf(
			"%w: %s (release or rename it, or choose a different cluster name)",
			ErrFloatingIPNotOwned, floatingIPName,
		)
	}

	return true, nil
}

// AttachFloatingIPToServer assigns the floating IP to the given server. It is
// idempotent: when the IP is already assigned to that server the call is a
// no-op, so a Create retry after a partial failure re-asserts the assignment
// without an API roundtrip.
func (p *Provider) AttachFloatingIPToServer(
	ctx context.Context,
	floatingIP *hcloud.FloatingIP,
	server *hcloud.Server,
) error {
	if p.client == nil {
		return provider.ErrProviderUnavailable
	}

	if floatingIP.Server != nil && floatingIP.Server.ID == server.ID {
		return nil
	}

	_, _, err := p.client.FloatingIP.Assign(ctx, floatingIP, server)
	if err != nil {
		return fmt.Errorf(
			"failed to assign floating IP %s to server %s: %w",
			floatingIP.Name, server.Name, err,
		)
	}

	return nil
}

// DetachFloatingIP unassigns the floating IP from whatever server it is
// assigned to. Unassigned IPs are a no-op, so the call is idempotent.
func (p *Provider) DetachFloatingIP(ctx context.Context, floatingIP *hcloud.FloatingIP) error {
	if p.client == nil {
		return provider.ErrProviderUnavailable
	}

	if floatingIP.Server == nil {
		return nil
	}

	_, _, err := p.client.FloatingIP.Unassign(ctx, floatingIP)
	if err != nil {
		return fmt.Errorf("failed to unassign floating IP %s: %w", floatingIP.Name, err)
	}

	return nil
}

// deleteFloatingIP deletes the cluster's floating IP when it exists and is
// ksail-owned. A floating IP that merely shares the name but lacks the
// ksail.owned label is left alone — reserved addresses the user manages
// themselves must never be released by cluster deletion (a released floating
// IP cannot be recovered). Lookup failures propagate after the transient
// retries: GetByName reports "not found" as a nil IP with a nil error, so a
// non-nil error is a real API failure, and swallowing it would silently leak
// a billed reserved address.
func (p *Provider) deleteFloatingIP(ctx context.Context, clusterName string) error {
	floatingIPName := clusterName + FloatingIPSuffix

	floatingIP, err := retryTransientHetznerOperation(
		ctx,
		DefaultTransientRetryCount,
		p.calculateRetryDelay,
		func() (*hcloud.FloatingIP, error) {
			floatingIP, _, getErr := p.client.FloatingIP.GetByName(ctx, floatingIPName)
			if getErr != nil {
				return nil, fmt.Errorf("failed to get floating IP %s: %w", floatingIPName, getErr)
			}

			return floatingIP, nil
		},
	)
	if err != nil {
		return err
	}

	if floatingIP == nil || floatingIP.Labels[LabelOwned] != LabelOwnedValue {
		return nil
	}

	_, deleteErr := p.client.FloatingIP.Delete(ctx, floatingIP)
	if deleteErr != nil {
		return fmt.Errorf("failed to delete floating IP %s: %w", floatingIPName, deleteErr)
	}

	return nil
}
