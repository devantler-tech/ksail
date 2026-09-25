package hetzner_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type setRulesTestAction struct {
	ID       int64             `json:"id"`
	Status   string            `json:"status"`
	Progress int               `json:"progress"`
	Error    map[string]string `json:"error,omitempty"`
}

// sentFirewallRule is one rule of a set_rules request body, as sent to the API.
type sentFirewallRule struct {
	Direction string   `json:"direction"`
	Protocol  string   `json:"protocol"`
	Port      *string  `json:"port"`
	SourceIPs []string `json:"source_ips"`
}

// firewallRepairServer records what the provider sent to set_rules.
type firewallRepairServer struct {
	*httptest.Server

	setRulesCalled atomic.Bool
	sentRules      atomic.Pointer[[]sentFirewallRule]
}

// ownedFirewallLabels are the labels KSail puts on a firewall it created for
// "test-cluster".
func ownedFirewallLabels() map[string]string {
	return hetzner.ResourceLabels("test-cluster")
}

// newSSHFirewallRepairServer serves an existing, owned Talos-only firewall for
// "test-cluster" and answers set_rules with a single action in actionStatus.
func newSSHFirewallRepairServer(t *testing.T, actionStatus string) *firewallRepairServer {
	t.Helper()

	return newFirewallRepairServer(t, []map[string]any{{
		"direction":  "in",
		"protocol":   "tcp",
		"port":       "50000",
		"source_ips": []string{"0.0.0.0/0"},
	}}, ownedFirewallLabels(), actionStatus)
}

// newFirewallRepairServer serves an existing firewall for "test-cluster" with the
// given rules and labels, and answers set_rules with a single action in
// actionStatus.
func newFirewallRepairServer(
	t *testing.T,
	rules []map[string]any,
	labels map[string]string,
	actionStatus string,
) *firewallRepairServer {
	t.Helper()

	repair := &firewallRepairServer{}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /firewalls", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(t, w, map[string]any{
			"firewalls": []map[string]any{{
				"id":     9,
				"name":   "test-cluster" + hetzner.FirewallSuffix,
				"labels": labels,
				"rules":  rules,
			}},
		})
	})
	mux.HandleFunc(
		"POST /firewalls/9/actions/set_rules",
		func(writer http.ResponseWriter, request *http.Request) {
			repair.setRulesCalled.Store(true)

			var body struct {
				Rules []sentFirewallRule `json:"rules"`
			}

			err := json.NewDecoder(request.Body).Decode(&body)
			if err != nil {
				t.Errorf("decode set_rules request: %v", err)
			}

			repair.sentRules.Store(&body.Rules)

			action := setRulesTestAction{ID: 77, Status: actionStatus, Progress: 100}
			if actionStatus == "error" {
				action.Error = map[string]string{
					"code":    "action_failed",
					"message": "rules not applied",
				}
			}

			writeJSONResponse(t, writer, map[string]any{"actions": []setRulesTestAction{action}})
		},
	)

	repair.Server = httptest.NewServer(mux)
	t.Cleanup(repair.Close)

	return repair
}

// sentRuleSummary reduces the rules sent to set_rules to "protocol/port" keys
// mapped to their sorted source CIDRs, so a test can assert them as a whole.
func (s *firewallRepairServer) sentRuleSummary(t *testing.T) map[string][]string {
	t.Helper()

	sent := s.sentRules.Load()
	require.NotNil(t, sent, "set_rules must have been called with a body")

	summary := make(map[string][]string, len(*sent))

	for _, rule := range *sent {
		assert.Equal(t, "in", rule.Direction)

		key := rule.Protocol
		if rule.Port != nil {
			key += "/" + *rule.Port
		}

		sources := append([]string(nil), rule.SourceIPs...)
		sort.Strings(sources)
		summary[key] = sources
	}

	return summary
}

func returnedPorts(firewall *hcloud.Firewall) []string {
	ports := make([]string, 0, len(firewall.Rules))

	for _, rule := range firewall.Rules {
		if rule.Port != nil {
			ports = append(ports, *rule.Port)
		}
	}

	sort.Strings(ports)

	return ports
}

func TestEnsureSSHFirewallRepairWaitsForSetRulesAction(t *testing.T) {
	t.Parallel()

	srv := newSSHFirewallRepairServer(t, "success")
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	firewall, err := prov.EnsureSSHFirewall(
		context.Background(), "test-cluster", []string{"203.0.113.0/24"},
	)

	require.NoError(t, err)
	require.NotNil(t, firewall)
	assert.True(t, srv.setRulesCalled.Load(), "a Talos-only firewall must be repaired")
	assert.Equal(t, map[string][]string{
		"tcp/22":   {"203.0.113.0/24"},
		"tcp/6443": {"203.0.113.0/24"},
		"icmp":     {"0.0.0.0/0", "::/0"},
	}, srv.sentRuleSummary(t), "SSH and the Kubernetes API follow the allowed CIDRs")
	assert.Equal(t, []string{"22", "6443"}, returnedPorts(firewall),
		"the returned firewall must carry the reconciled rules")
}

func TestEnsureSSHFirewallRepairReportsFailedSetRulesAction(t *testing.T) {
	t.Parallel()

	srv := newSSHFirewallRepairServer(t, "error")
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	firewall, err := prov.EnsureSSHFirewall(context.Background(), "test-cluster", nil)

	require.Error(t, err, "a failed rule update must not be reported as a repaired firewall")
	assert.Contains(t, err.Error(), "failed to apply firewall rules")
	assert.Nil(t, firewall)
	assert.True(t, srv.setRulesCalled.Load())
}

// TestEnsureFirewallRepairsStaleSSHFirewall catches a Talos create reusing a
// firewall left behind by an interrupted Vanilla or K3s create of the same name,
// which allows SSH but not the Talos API the create then depends on.
func TestEnsureFirewallRepairsStaleSSHFirewall(t *testing.T) {
	t.Parallel()

	srv := newFirewallRepairServer(t, []map[string]any{{
		"direction":  "in",
		"protocol":   "tcp",
		"port":       "22",
		"source_ips": []string{"0.0.0.0/0"},
	}}, ownedFirewallLabels(), "success")
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	firewall, err := prov.EnsureFirewall(context.Background(), "test-cluster", nil)

	require.NoError(t, err)
	require.NotNil(t, firewall)
	assert.True(t, srv.setRulesCalled.Load(), "an SSH-only firewall must be given the Talos rules")
	assert.Equal(t, map[string][]string{
		"tcp/50000": {"0.0.0.0/0", "::/0"},
		"tcp/6443":  {"0.0.0.0/0", "::/0"},
		"icmp":      {"0.0.0.0/0", "::/0"},
	}, srv.sentRuleSummary(t), "a Talos firewall opens the Talos API, not SSH")
	assert.Equal(t, []string{"50000", "6443"}, returnedPorts(firewall),
		"the returned firewall must carry the reconciled rules")
}

func TestEnsureFirewallRepairReportsFailedSetRulesAction(t *testing.T) {
	t.Parallel()

	srv := newFirewallRepairServer(t, []map[string]any{{
		"direction":  "in",
		"protocol":   "tcp",
		"port":       "22",
		"source_ips": []string{"0.0.0.0/0"},
	}}, ownedFirewallLabels(), "error")
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	firewall, err := prov.EnsureFirewall(context.Background(), "test-cluster", nil)

	require.Error(t, err, "a failed rule update must not be reported as a repaired firewall")
	assert.Contains(t, err.Error(), "failed to apply firewall rules")
	assert.Nil(t, firewall)
	assert.True(t, srv.setRulesCalled.Load())
}

// TestEnsureFirewallRefusesUnownedFirewall catches a create overwriting the rules of
// a firewall that merely shares the cluster firewall's name: one with no KSail
// labels, and one KSail created for a different cluster.
func TestEnsureFirewallRefusesUnownedFirewall(t *testing.T) {
	t.Parallel()

	for name, labels := range map[string]map[string]string{
		"unlabelled":    nil,
		"other cluster": hetzner.ResourceLabels("other-cluster"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newFirewallRepairServer(t, []map[string]any{{
				"direction":  "in",
				"protocol":   "tcp",
				"port":       "50000",
				"source_ips": []string{"0.0.0.0/0"},
			}}, labels, "success")
			prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

			firewall, err := prov.EnsureSSHFirewall(context.Background(), "test-cluster", nil)

			require.ErrorIs(t, err, hetzner.ErrFirewallNotOwned)
			assert.Nil(t, firewall)
			assert.False(t, srv.setRulesCalled.Load(),
				"the rules of a firewall KSail does not own must not be changed")
		})
	}
}
