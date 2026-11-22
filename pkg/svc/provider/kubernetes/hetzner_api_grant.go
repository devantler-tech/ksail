package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	// ErrInvalidHetznerHost identifies an ambiguous host identity that cannot authorize a grant.
	ErrInvalidHetznerHost = errors.New("invalid Hetzner host identity")
	// ErrHetznerGrantCredentials means the host nodes are Hetzner servers but no Hetzner
	// credential is available to read their firewall, so deletion stops instead of possibly
	// leaving a nested API port open.
	ErrHetznerGrantCredentials = errors.New(
		"hetzner credential required to revoke nested API grant",
	)
)

// WithHetznerAPIFirewall enables source-restricted grants for owned Hetzner host nodes.
// Configure it before sharing the provider. Nil grants nothing and leaves exposure unchanged on
// hosts without Hetzner nodes. On a Hetzner host the firewall cannot be read without it, so a
// tier that would release a granted NodePort (Gateway, LoadBalancer) fails and exposure falls
// back to NodePort, and deletion stops (ErrHetznerGrantCredentials).
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

// priorNestedAPIGrant reports whether the owned host firewall already admits the nested
// cluster in namespace. The firewall is the only record of a grant, so a resolution reads it
// before any tier runs and compensation never removes a grant that predates the attempt.
func (p *Provider) priorNestedAPIGrant(ctx context.Context, namespace string) (bool, error) {
	if p == nil || p.hetznerClient == nil {
		return false, nil
	}

	host, err := p.ownedHetznerHost(ctx, false)
	if err != nil || host == "" {
		return false, err
	}

	granted, err := hetzner.NewProvider(p.hetznerClient).HasNestedAPIGrant(ctx, host, namespace)
	if err != nil {
		return false, fmt.Errorf("read nested API grant: %w", err)
	}

	return granted, nil
}

// grantNestedAPI admits the NodePort of the nested cluster in namespace on its owned Hetzner
// host. The grant is keyed by namespace, which is unique on the host. Compensation removes
// it only when this attempt created it and the firewall held no grant for the namespace
// before the resolution started (prior), so a failed duplicate create keeps a running
// cluster's grant.
func (p *Provider) grantNestedAPI(
	ctx context.Context,
	namespace string,
	prior bool,
	result *ExposureResult,
) error {
	host, err := p.ownedHetznerHost(ctx, true)
	if err != nil || host == "" {
		return err
	}

	cloud := hetzner.NewProvider(p.hetznerClient)

	created, err := cloud.GrantNestedAPIPort(ctx, host, namespace, int(result.Port))
	if created && !prior {
		result.cleanupGrant = func(ctx context.Context) error {
			err := cloud.RevokeNestedAPIPort(ctx, host, namespace)
			if err != nil {
				return fmt.Errorf("revoke nested API access: %w", err)
			}

			return nil
		}
	}

	if err != nil {
		// A failed action wait can follow an applied rule. Retain both failures if
		// compensating cleanup cannot establish removal.
		err = result.CleanupOnFailure(ctx, err)

		return fmt.Errorf("grant nested API access: %w", err)
	}

	return nil
}

// RevokeNestedAPIGrant removes the grant of the nested cluster in namespace. It discovers
// the host independently of the namespace, so deletion can retry after a failed create or
// after namespace removal. On a host whose nodes are Hetzner servers it needs a Hetzner
// credential, because only the firewall can tell whether a grant exists.
func (p *Provider) RevokeNestedAPIGrant(ctx context.Context, namespace string) error {
	if p == nil {
		return nil
	}

	if p.hetznerClient == nil {
		return p.requireNoHetznerHost(ctx, namespace)
	}

	host, err := p.ownedHetznerHost(ctx, false)
	if err != nil || host == "" {
		// No owned host firewall means KSail never granted this namespace anything.
		return err
	}

	err = hetzner.NewProvider(p.hetznerClient).RevokeNestedAPIPort(ctx, host, namespace)
	if err != nil {
		return fmt.Errorf("revoke nested API access: %w", err)
	}

	return nil
}

// requireNoHetznerHost succeeds only when no host node is a Hetzner server. Without a
// credential the firewall cannot be read, so a Hetzner host may still admit namespace.
func (p *Provider) requireNoHetznerHost(ctx context.Context, namespace string) error {
	nodes, err := p.hostNodes(ctx)
	if err != nil {
		return err
	}

	for _, node := range nodes {
		if strings.HasPrefix(node.Spec.ProviderID, "hcloud://") {
			return fmt.Errorf(
				"%w: host node %q is a Hetzner server whose firewall may admit %s; "+
					"set %s and retry",
				ErrHetznerGrantCredentials,
				node.Name,
				namespace,
				v1alpha1.DefaultHetznerTokenEnvVar,
			)
		}
	}

	return nil
}

// hostNodes lists the host nodes for a grant decision. An identity that may not read nodes
// cannot prove a Hetzner host, so a forbidden read yields no nodes: KSail then neither grants
// nor requires a Hetzner credential, and ordinary exposure and deletion keep working.
func (p *Provider) hostNodes(ctx context.Context) ([]corev1.Node, error) {
	nodes, err := p.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if apierrors.IsForbidden(err) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read host nodes for nested API grant: %w", err)
	}

	return nodes.Items, nil
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
	ids, err := p.hetznerHostNodeIDs(ctx, requireAttached)
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

// hetznerHostNodeIDs reads the host nodes' Hetzner server IDs. A grant needs every node to be
// a Hetzner server, so any other node means no grant. A revoke skips nodes without a Hetzner
// identity, such as a node that is still joining, so it cannot leave an opened port behind;
// the remaining servers and the firewall must still prove ownership.
func (p *Provider) hetznerHostNodeIDs(ctx context.Context, requireAll bool) ([]int64, error) {
	if p == nil || p.hetznerClient == nil {
		return nil, nil
	}

	nodes, err := p.hostNodes(ctx)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(nodes))

	for _, node := range nodes {
		text, ok := strings.CutPrefix(node.Spec.ProviderID, "hcloud://")
		if !ok {
			if requireAll {
				return nil, nil
			}

			continue
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
