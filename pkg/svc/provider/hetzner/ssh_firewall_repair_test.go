package hetzner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type setRulesTestAction struct {
	ID       int64             `json:"id"`
	Status   string            `json:"status"`
	Progress int               `json:"progress"`
	Error    map[string]string `json:"error,omitempty"`
}

// newSSHFirewallRepairServer serves an existing Talos-only firewall for
// "test-cluster" and answers set_rules with a single action in actionStatus.
func newSSHFirewallRepairServer(
	t *testing.T,
	actionStatus string,
	setRulesCalled *atomic.Bool,
) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /firewalls", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(t, w, map[string]any{
			"firewalls": []map[string]any{{
				"id":   9,
				"name": "test-cluster" + hetzner.FirewallSuffix,
				"rules": []map[string]any{{
					"direction":  "in",
					"protocol":   "tcp",
					"port":       "50000",
					"source_ips": []string{"0.0.0.0/0"},
				}},
			}},
		})
	})
	mux.HandleFunc("POST /firewalls/9/actions/set_rules", func(writer http.ResponseWriter, _ *http.Request) {
		setRulesCalled.Store(true)

		action := setRulesTestAction{ID: 77, Status: actionStatus, Progress: 100}
		if actionStatus == "error" {
			action.Error = map[string]string{"code": "action_failed", "message": "rules not applied"}
		}

		writeJSONResponse(t, writer, map[string]any{"actions": []setRulesTestAction{action}})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func TestEnsureSSHFirewallRepairWaitsForSetRulesAction(t *testing.T) {
	t.Parallel()

	var setRulesCalled atomic.Bool

	srv := newSSHFirewallRepairServer(t, "success", &setRulesCalled)
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	firewall, err := prov.EnsureSSHFirewall(context.Background(), "test-cluster", nil)

	require.NoError(t, err)
	assert.NotNil(t, firewall)
	assert.True(t, setRulesCalled.Load(), "a Talos-only firewall must be repaired")
}

func TestEnsureSSHFirewallRepairReportsFailedSetRulesAction(t *testing.T) {
	t.Parallel()

	var setRulesCalled atomic.Bool

	srv := newSSHFirewallRepairServer(t, "error", &setRulesCalled)
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	firewall, err := prov.EnsureSSHFirewall(context.Background(), "test-cluster", nil)

	require.Error(t, err, "a failed rule update must not be reported as a repaired firewall")
	assert.Contains(t, err.Error(), "failed to apply firewall rules")
	assert.Nil(t, firewall)
	assert.True(t, setRulesCalled.Load())
}
