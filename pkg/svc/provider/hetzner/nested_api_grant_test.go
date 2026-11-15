package hetzner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const nestedGrantPrefix = "KSail nested API: "

// hostAPIRule is the host's own Kubernetes API rule, restricted to one CIDR.
func hostAPIRule() map[string]any {
	return map[string]any{
		"direction":  "in",
		"protocol":   "tcp",
		"port":       "6443",
		"source_ips": []string{"203.0.113.0/24"},
	}
}

// nestedGrantRule is an existing grant for nested with a deliberately wide source, so a
// test can see whether the provider re-derives it from the host API rule.
func nestedGrantRule(nested, port string) map[string]any {
	return map[string]any{
		"direction":   "in",
		"protocol":    "tcp",
		"port":        port,
		"source_ips":  []string{"0.0.0.0/0"},
		"description": nestedGrantPrefix + nested,
	}
}

// sentRuleDetails reduces the rules sent to set_rules to port keys mapped to their sorted
// sources. Rules without a port are keyed by protocol; a nested grant's key adds its
// description, so a test sees which nested cluster each grant belongs to.
func (s *firewallRepairServer) sentRuleDetails(t *testing.T) map[string][]string {
	t.Helper()

	sent := s.sentRules.Load()
	require.NotNil(t, sent, "set_rules must have been called with a body")

	details := make(map[string][]string, len(*sent))

	for _, rule := range *sent {
		key := rule.Protocol
		if rule.Port != nil {
			key = *rule.Port
		}

		if rule.Description != nil && strings.HasPrefix(*rule.Description, nestedGrantPrefix) {
			key += " " + *rule.Description
		}

		sources := append([]string(nil), rule.SourceIPs...)
		sort.Strings(sources)
		details[key] = sources
	}

	return details
}

func TestGrantNestedAPIPortAddsSourceRestrictedRule(t *testing.T) {
	t.Parallel()

	srv := newFirewallRepairServer(t, []map[string]any{
		hostAPIRule(),
		nestedGrantRule("other", "31000"),
	}, ownedFirewallLabels(), "success")
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	err := prov.GrantNestedAPIPort(context.Background(), "test-cluster", "nested-kind", 30668)

	require.NoError(t, err)
	assert.Equal(t, map[string][]string{
		"6443":                                 {"203.0.113.0/24"},
		"31000 " + nestedGrantPrefix + "other": {"0.0.0.0/0"},
		"30668 " + nestedGrantPrefix + "nested-kind": {"203.0.113.0/24"},
	}, srv.sentRuleDetails(t),
		"the new grant inherits the host API sources and other rules are kept unchanged")
}

func TestGrantNestedAPIPortReplacesEarlierGrant(t *testing.T) {
	t.Parallel()

	srv := newFirewallRepairServer(t, []map[string]any{
		hostAPIRule(),
		nestedGrantRule("nested-kind", "30001"),
	}, ownedFirewallLabels(), "success")
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	err := prov.GrantNestedAPIPort(context.Background(), "test-cluster", "nested-kind", 30668)

	require.NoError(t, err)
	assert.Equal(t, map[string][]string{
		"6443": {"203.0.113.0/24"},
		"30668 " + nestedGrantPrefix + "nested-kind": {"203.0.113.0/24"},
	}, srv.sentRuleDetails(t), "a nested cluster holds at most one grant")
}

func TestGrantNestedAPIPortRefusals(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		rules   []map[string]any
		labels  map[string]string
		nested  string
		port    int
		wantErr error
	}{
		"unowned firewall": {
			rules: []map[string]any{hostAPIRule()}, labels: nil,
			nested: "nested-kind", port: 30668, wantErr: hetzner.ErrFirewallNotOwned,
		},
		"no Kubernetes API rule": {
			rules: []map[string]any{
				nestedGrantRule("other", "31000"),
			},
			labels:  ownedFirewallLabels(),
			nested:  "nested-kind",
			port:    30668,
			wantErr: hetzner.ErrNoKubernetesAPIRule,
		},
		"empty nested name": {
			rules: []map[string]any{hostAPIRule()}, labels: ownedFirewallLabels(),
			nested: "", port: 30668, wantErr: hetzner.ErrInvalidNestedAPIGrant,
		},
		"port out of range": {
			rules: []map[string]any{hostAPIRule()}, labels: ownedFirewallLabels(),
			nested: "nested-kind", port: 70000, wantErr: hetzner.ErrInvalidNestedAPIGrant,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newFirewallRepairServer(t, test.rules, test.labels, "success")
			prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

			err := prov.GrantNestedAPIPort(
				context.Background(), "test-cluster", test.nested, test.port,
			)

			require.ErrorIs(t, err, test.wantErr)
			assert.False(t, srv.setRulesCalled.Load(), "a refused grant must not change any rule")
		})
	}
}

func TestGrantNestedAPIPortRefusesMissingFirewall(t *testing.T) {
	t.Parallel()

	prov := hetzner.NewProvider(newTestHcloudClient(t, newNoFirewallServer(t)))

	err := prov.GrantNestedAPIPort(context.Background(), "test-cluster", "nested-kind", 30668)

	require.ErrorIs(t, err, hetzner.ErrHostFirewallNotFound)
}

func TestRevokeNestedAPIPortRemovesOnlyThatGrant(t *testing.T) {
	t.Parallel()

	srv := newFirewallRepairServer(t, []map[string]any{
		hostAPIRule(),
		nestedGrantRule("nested-kind", "30668"),
		nestedGrantRule("other", "31000"),
	}, ownedFirewallLabels(), "success")
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	err := prov.RevokeNestedAPIPort(context.Background(), "test-cluster", "nested-kind")

	require.NoError(t, err)
	assert.Equal(t, map[string][]string{
		"6443":                                 {"203.0.113.0/24"},
		"31000 " + nestedGrantPrefix + "other": {"0.0.0.0/0"},
	}, srv.sentRuleDetails(t))
}

func TestRevokeNestedAPIPortIsNoOpWithoutGrantOrFirewall(t *testing.T) {
	t.Parallel()

	srv := newFirewallRepairServer(t, []map[string]any{
		hostAPIRule(),
		nestedGrantRule("other", "31000"),
	}, ownedFirewallLabels(), "success")
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	require.NoError(
		t,
		prov.RevokeNestedAPIPort(context.Background(), "test-cluster", "nested-kind"),
	)
	assert.False(t, srv.setRulesCalled.Load(), "revoking an absent grant changes nothing")

	missing := hetzner.NewProvider(newTestHcloudClient(t, newNoFirewallServer(t)))
	require.NoError(
		t,
		missing.RevokeNestedAPIPort(context.Background(), "test-cluster", "nested-kind"),
	)
}

// TestEnsureFirewallKeepsNestedGrants catches a host create or update removing the API
// grants of nested clusters it hosts, and checks the kept grants follow the host's
// current allowed CIDRs instead of keeping a wider source.
func TestEnsureFirewallKeepsNestedGrants(t *testing.T) {
	t.Parallel()

	srv := newFirewallRepairServer(t, []map[string]any{
		hostAPIRule(),
		nestedGrantRule("nested-kind", "30668"),
	}, ownedFirewallLabels(), "success")
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	firewall, err := prov.EnsureFirewall(
		context.Background(), "test-cluster", []string{"198.51.100.0/24"},
	)

	require.NoError(t, err)
	require.NotNil(t, firewall)
	assert.Equal(t, map[string][]string{
		"50000": {"198.51.100.0/24"},
		"6443":  {"198.51.100.0/24"},
		"icmp":  {"0.0.0.0/0", "::/0"},
		"30668 " + nestedGrantPrefix + "nested-kind": {"198.51.100.0/24"},
	}, srv.sentRuleDetails(t))
	assert.Equal(t, []string{"30668", "50000", "6443"}, returnedPorts(firewall))
}

func TestSyncFirewallRulesKeepsNestedGrants(t *testing.T) {
	t.Parallel()

	srv := newFirewallRepairServer(t, []map[string]any{
		hostAPIRule(),
		nestedGrantRule("nested-kind", "30668"),
	}, ownedFirewallLabels(), "success")
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	err := prov.SyncFirewallRules(context.Background(), "test-cluster", nil)

	require.NoError(t, err)
	assert.Equal(t, map[string][]string{
		"50000": {"0.0.0.0/0", "::/0"},
		"6443":  {"0.0.0.0/0", "::/0"},
		"icmp":  {"0.0.0.0/0", "::/0"},
		"30668 " + nestedGrantPrefix + "nested-kind": {"0.0.0.0/0", "::/0"},
	}, srv.sentRuleDetails(t))
}

// newNoFirewallServer serves an hcloud API with no firewalls and returns its URL.
func newNoFirewallServer(t *testing.T) string {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /firewalls", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(t, w, map[string]any{"firewalls": []map[string]any{}})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv.URL
}
