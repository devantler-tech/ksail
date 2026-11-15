package hetzner_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statefulFirewallServer serves one owned firewall for "test-cluster" whose rules
// set_rules replaces, so overlapping read-build-write sequences can be observed.
type statefulFirewallServer struct {
	*httptest.Server

	mu    sync.Mutex
	rules []schema.FirewallRule
}

func newStatefulFirewallServer(t *testing.T, readDelay time.Duration) *statefulFirewallServer {
	t.Helper()

	sourceIPs := []string{"203.0.113.0/24"}
	state := &statefulFirewallServer{rules: []schema.FirewallRule{{
		Direction: "in",
		Protocol:  "tcp",
		Port:      new("6443"),
		SourceIPs: sourceIPs,
	}}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /firewalls", func(writer http.ResponseWriter, _ *http.Request) {
		state.mu.Lock()
		rules := append([]schema.FirewallRule(nil), state.rules...)
		state.mu.Unlock()

		// Answer late, so a second caller can read the same snapshot before this one writes.
		time.Sleep(readDelay)

		writeJSONResponse(t, writer, map[string]any{
			"firewalls": []map[string]any{{
				"id":     9,
				"name":   "test-cluster" + hetzner.FirewallSuffix,
				"labels": ownedFirewallLabels(),
				"rules":  rules,
			}},
		})
	})
	mux.HandleFunc(
		"POST /firewalls/9/actions/set_rules",
		func(writer http.ResponseWriter, request *http.Request) {
			state.setRules(t, writer, request)
		},
	)

	state.Server = httptest.NewServer(mux)
	t.Cleanup(state.Close)

	return state
}

func (s *statefulFirewallServer) grantedNestedClusters() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	granted := []string{}

	for _, rule := range s.rules {
		if rule.Description != nil {
			granted = append(granted, *rule.Description)
		}
	}

	return granted
}

func TestGrantNestedAPIPortConcurrentGrantsAreAllKept(t *testing.T) {
	t.Parallel()

	srv := newStatefulFirewallServer(t, 50*time.Millisecond)

	const grants = 4

	var workers sync.WaitGroup

	errs := make([]error, grants)

	for i := range grants {
		workers.Go(func() {
			// Each grant uses its own Provider: the guard must hold across instances.
			prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))
			errs[i] = prov.GrantNestedAPIPort(
				context.Background(), "test-cluster", "nested-"+strconv.Itoa(i), 30000+i,
			)
		})
	}

	workers.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}

	assert.ElementsMatch(t, []string{
		nestedGrantPrefix + "nested-0",
		nestedGrantPrefix + "nested-1",
		nestedGrantPrefix + "nested-2",
		nestedGrantPrefix + "nested-3",
	}, srv.grantedNestedClusters(), "no grant may be lost to an overlapping rule write")
}

// setRules replaces the served rules with the ones the request sends.
func (s *statefulFirewallServer) setRules(
	t *testing.T,
	writer http.ResponseWriter,
	request *http.Request,
) {
	t.Helper()

	var body schema.FirewallActionSetRulesRequest

	err := json.NewDecoder(request.Body).Decode(&body)
	if err != nil {
		t.Errorf("decode set_rules request: %v", err)
	}

	rules := make([]schema.FirewallRule, 0, len(body.Rules))
	for _, rule := range body.Rules {
		rules = append(rules, schema.FirewallRule{
			Direction:   rule.Direction,
			Protocol:    rule.Protocol,
			Port:        rule.Port,
			SourceIPs:   rule.SourceIPs,
			Description: rule.Description,
		})
	}

	s.mu.Lock()
	s.rules = rules
	s.mu.Unlock()

	action := setRulesTestAction{ID: 77, Status: "success", Progress: 100}
	writeJSONResponse(t, writer, map[string]any{"actions": []setRulesTestAction{action}})
}
