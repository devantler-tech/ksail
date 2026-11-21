package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ErrInvalidHetznerHost identifies an ambiguous host identity that cannot authorize a grant.
var ErrInvalidHetznerHost = errors.New("invalid Hetzner host identity")

// WithHetznerAPIFirewall enables source-restricted grants for owned Hetzner host nodes.
// Configure it before sharing the provider; nil leaves ordinary exposure unchanged.
func (p *Provider) WithHetznerAPIFirewall(client *hcloud.Client) *Provider {
	p.hetznerClient = client

	return p
}

// WithHetznerAPIFirewallFromEnvironment uses the same optional credential as cloud discovery.
// No token means no cloud reads or mutations, preserving other Kubernetes hosts' behavior.
func (p *Provider) WithHetznerAPIFirewallFromEnvironment() *Provider {
	token := (credentials.EnvResolver{}).Value(credentials.HetznerToken)
	if token != "" {
		p.hetznerClient = hcloud.NewClient(hcloud.WithToken(token))
	}

	return p
}

func (p *Provider) grantNestedAPI(
	ctx context.Context,
	clusterName string,
	result *ExposureResult,
) error {
	host, err := p.ownedHetznerHost(ctx, true)
	if err != nil || host == "" {
		return err
	}

	cloud := hetzner.NewProvider(p.hetznerClient)
	result.cleanupGrant = func(ctx context.Context) error {
		return cloud.RevokeNestedAPIPort(ctx, host, clusterName)
	}

	err = cloud.GrantNestedAPIPort(ctx, host, clusterName, int(result.Port))
	if err != nil {
		// A failed action wait can follow an applied rule. Retain both failures if
		// compensating cleanup cannot establish removal.
		err = result.CleanupOnFailure(ctx, err)

		return fmt.Errorf("grant nested API access: %w", err)
	}

	return nil
}

// RevokeNestedAPIGrant discovers the host independently of the child's namespace,
// so deletion can retry after a failed create or after namespace removal.
func (p *Provider) RevokeNestedAPIGrant(ctx context.Context, clusterName string) error {
	host, err := p.ownedHetznerHost(ctx, false)
	if err != nil || host == "" {
		return err
	}

	err = hetzner.NewProvider(p.hetznerClient).RevokeNestedAPIPort(ctx, host, clusterName)
	if err != nil {
		return fmt.Errorf("revoke nested API access: %w", err)
	}

	return nil
}

const nestedGrantCleanupTimeout = 30 * time.Second

// CleanupOnFailure compensates only an exposure that actually granted access.
// It preserves the create error and bounds cleanup independently of cancelled readiness.
func (r *ExposureResult) CleanupOnFailure(ctx context.Context, createErr error) error {
	if r == nil || r.cleanupGrant == nil || createErr == nil {
		return createErr
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), nestedGrantCleanupTimeout)
	defer cancel()

	err := r.cleanupGrant(cleanupCtx)
	if err != nil {
		return errors.Join(createErr, fmt.Errorf("cleanup nested API grant: %w", err))
	}

	return createErr
}

// ownedHetznerHost requires all host nodes to belong to one KSail cluster and
// have its owned firewall applied. Names and addresses alone grant no authority.
func (p *Provider) ownedHetznerHost(ctx context.Context, requireAttached bool) (string, error) {
	ids, err := p.hetznerHostNodeIDs(ctx)
	if err != nil || len(ids) == 0 {
		return "", err
	}

	host, servers, err := p.ownedHetznerServers(ctx, ids)
	if err != nil || host == "" {
		return "", err
	}

	firewall, _, err := p.hetznerClient.Firewall.GetByName(ctx, host+hetzner.FirewallSuffix)
	if err != nil {
		return "", fmt.Errorf("read Hetzner host firewall: %w", err)
	}

	if !ownedHostFirewall(firewall, host) ||
		(requireAttached && !hostFirewallApplied(servers, firewall.ID)) {
		return "", nil
	}

	return host, nil
}

func (p *Provider) hetznerHostNodeIDs(ctx context.Context) ([]int64, error) {
	if p == nil || p.hetznerClient == nil {
		return nil, nil
	}

	nodes, err := p.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("read host nodes for nested API grant: %w", err)
	}

	ids := make([]int64, 0, len(nodes.Items))

	for _, node := range nodes.Items {
		text, ok := strings.CutPrefix(node.Spec.ProviderID, "hcloud://")
		if !ok {
			return nil, nil
		}

		id, parseErr := strconv.ParseInt(text, 10, 64)
		if parseErr != nil || id <= 0 || strconv.FormatInt(id, 10) != text {
			return nil, ErrInvalidHetznerHost
		}

		ids = append(ids, id)
	}

	return ids, nil
}

func ownedHostFirewall(firewall *hcloud.Firewall, host string) bool {
	return firewall != nil && firewall.Labels[hetzner.LabelOwned] == hetzner.LabelOwnedValue &&
		firewall.Labels[hetzner.LabelClusterName] == host
}

func hostFirewallApplied(servers []*hcloud.Server, firewallID int64) bool {
	for _, server := range servers {
		attached := false

		for _, status := range server.PublicNet.Firewalls {
			if status != nil && status.Firewall.ID == firewallID &&
				status.Status == hcloud.FirewallStatusApplied {
				attached = true

				break
			}
		}

		if !attached {
			return false
		}
	}

	return true
}

func (p *Provider) ownedHetznerServers(
	ctx context.Context,
	ids []int64,
) (string, []*hcloud.Server, error) {
	var host string

	servers := make([]*hcloud.Server, 0, len(ids))

	for _, id := range ids {
		server, _, getErr := p.hetznerClient.Server.GetByID(ctx, id)
		if getErr != nil {
			return "", nil, fmt.Errorf("read Hetzner host server: %w", getErr)
		}

		if server == nil || server.Labels[hetzner.LabelOwned] != hetzner.LabelOwnedValue {
			return "", nil, nil
		}

		name := server.Labels[hetzner.LabelClusterName]
		if name == "" || (host != "" && host != name) {
			return "", nil, ErrInvalidHetznerHost
		}

		host = name

		servers = append(servers, server)
	}

	return host, servers, nil
}
