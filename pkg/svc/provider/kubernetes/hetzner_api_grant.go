package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// HetznerAPIGrantAnnotation records, on a nested cluster's API Service, the Hetzner host whose
// firewall admits that cluster's API port. Deletion reads it to tell a recorded grant from a
// host that never received one.
const HetznerAPIGrantAnnotation = "ksail.io/hetzner-api-grant"

var (
	// ErrInvalidHetznerHost identifies an ambiguous host identity that cannot authorize a grant.
	ErrInvalidHetznerHost = errors.New("invalid Hetzner host identity")
	// ErrHetznerGrantCredentials means a recorded grant exists but no Hetzner credential is
	// available to remove it, so deletion stops instead of leaving the port open.
	ErrHetznerGrantCredentials = errors.New(
		"hetzner credential required to revoke nested API grant",
	)
)

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

// grantNestedAPI admits the NodePort of the nested cluster in namespace on its owned Hetzner
// host. The grant is keyed by namespace, which is unique on the host. Compensation removes
// it only when this attempt created it, so a failed duplicate create keeps a running
// cluster's grant.
func (p *Provider) grantNestedAPI(
	ctx context.Context,
	namespace string,
	result *ExposureResult,
) error {
	host, err := p.ownedHetznerHost(ctx, true)
	if err != nil || host == "" {
		return err
	}

	cloud := hetzner.NewProvider(p.hetznerClient)

	created, err := cloud.GrantNestedAPIPort(ctx, host, namespace, int(result.Port))
	if created {
		result.cleanupGrant = func(ctx context.Context) error {
			return cloud.RevokeNestedAPIPort(ctx, host, namespace)
		}
	}

	if err == nil {
		err = p.recordNestedAPIGrant(ctx, namespace, host)
	}

	if err != nil {
		// A failed action wait can follow an applied rule. Retain both failures if
		// compensating cleanup cannot establish removal.
		err = result.CleanupOnFailure(ctx, err)

		return fmt.Errorf("grant nested API access: %w", err)
	}

	return nil
}

func (p *Provider) recordNestedAPIGrant(ctx context.Context, namespace, host string) error {
	var record struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}

	record.Metadata.Annotations = map[string]string{HetznerAPIGrantAnnotation: host}

	patch, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode nested API grant record: %w", err)
	}

	_, err = p.client.CoreV1().Services(namespace).
		Patch(ctx, APIServiceName, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("record nested API grant: %w", err)
	}

	return nil
}

// RevokeNestedAPIGrant removes the grant of the nested cluster in namespace. It discovers
// the host independently of the namespace, so deletion can retry after a failed create or
// after namespace removal. Without a Hetzner credential it succeeds only when no grant is
// recorded on the namespace's API Service.
func (p *Provider) RevokeNestedAPIGrant(ctx context.Context, namespace string) error {
	if p == nil {
		return nil
	}

	if p.hetznerClient == nil {
		return p.requireNoRecordedGrant(ctx, namespace)
	}

	host, err := p.ownedHetznerHost(ctx, false)
	if err != nil || host == "" {
		return err
	}

	err = hetzner.NewProvider(p.hetznerClient).RevokeNestedAPIPort(ctx, host, namespace)
	if err != nil {
		return fmt.Errorf("revoke nested API access: %w", err)
	}

	return nil
}

func (p *Provider) requireNoRecordedGrant(ctx context.Context, namespace string) error {
	svc, err := p.client.CoreV1().Services(namespace).Get(ctx, APIServiceName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read nested API grant record: %w", err)
	}

	host := svc.Annotations[HetznerAPIGrantAnnotation]
	if host == "" {
		return nil
	}

	return fmt.Errorf(
		"%w: the firewall of Hetzner host %q admits %s; set %s and retry",
		ErrHetznerGrantCredentials, host, namespace, v1alpha1.DefaultHetznerTokenEnvVar,
	)
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

	nodes, err := p.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("read host nodes for nested API grant: %w", err)
	}

	ids := make([]int64, 0, len(nodes.Items))

	for _, node := range nodes.Items {
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
