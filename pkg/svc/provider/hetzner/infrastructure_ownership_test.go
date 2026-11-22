package hetzner_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	ownershipCluster = "test-cluster"
	ownedLabels      = `{"ksail.owned":"true","ksail.cluster.name":"test-cluster"}`
	foreignLabels    = `{"team":"someone-else"}`
	otherCluster     = `{"ksail.owned":"true","ksail.cluster.name":"other-cluster"}`
	noClusterLabel   = `{"ksail.owned":"true"}`
	ccmLBLabels      = `{"hcloud-ccm/service-uid":"0f3a"}`
	forbiddenReply   = `{"error":{"code":"forbidden","message":"insufficient permissions"}}`
)

// ownershipResource describes one Hetzner resource kind served by the fake API:
// its collection path, the JSON key of its list response, and how one item looks.
type ownershipResource struct {
	collection string
	listKey    string
	item       func(labels string) string
	remove     func(p *hetzner.Provider, ctx context.Context) error
}

func ownershipResources() map[string]ownershipResource {
	return map[string]ownershipResource{
		"placement group": {
			collection: "/placement_groups",
			listKey:    "placement_groups",
			item: func(labels string) string {
				return `{"id":11,"name":"test-cluster-placement","type":"spread","servers":[],"labels":` +
					labels + `}`
			},
			remove: func(p *hetzner.Provider, ctx context.Context) error {
				return p.DeletePlacementGroupForTest(ctx, ownershipCluster)
			},
		},
		"firewall": {
			collection: "/firewalls",
			listKey:    "firewalls",
			item: func(labels string) string {
				return `{"id":11,"name":"test-cluster-firewall","rules":[],"applied_to":[],"labels":` +
					labels + `}`
			},
			remove: func(p *hetzner.Provider, ctx context.Context) error {
				return p.DeleteFirewallForTest(ctx, ownershipCluster)
			},
		},
		"network": {
			collection: "/networks",
			listKey:    "networks",
			item: func(labels string) string {
				return `{"id":11,"name":"test-cluster-network","ip_range":"10.0.0.0/16",` +
					`"subnets":[],"routes":[],"servers":[],"load_balancers":[],"labels":` + labels + `}`
			},
			remove: func(p *hetzner.Provider, ctx context.Context) error {
				return p.DeleteNetworkForTest(ctx, ownershipCluster)
			},
		},
	}
}

// newOwnershipServer serves a single resource of the given kind with the given
// labels (or a forbidden lookup error when lookupFails), and counts DELETE calls.
func newOwnershipServer(
	t *testing.T,
	res ownershipResource,
	labels string,
	lookupFails bool,
	deletes *int32,
) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc(res.collection, func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Content-Type", "application/json")

		if lookupFails {
			responseWriter.WriteHeader(http.StatusForbidden)
			_, _ = responseWriter.Write([]byte(forbiddenReply))

			return
		}

		_, _ = responseWriter.Write([]byte(`{"` + res.listKey + `":[` + res.item(labels) + `]}`))
	})
	mux.HandleFunc(
		res.collection+"/11",
		func(responseWriter http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodDelete {
				http.Error(responseWriter, "unexpected method", http.StatusMethodNotAllowed)

				return
			}

			atomic.AddInt32(deletes, 1)
			responseWriter.WriteHeader(http.StatusNoContent)
		},
	)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

// TestDeleteInfrastructure_OnlyRemovesKSailOwnedResources proves that cluster
// cleanup deletes a same-named placement group, firewall or network only when
// it carries the KSail ownership label, and fails instead of guessing when the
// lookup itself fails.
func TestDeleteInfrastructure_OnlyRemovesKSailOwnedResources(t *testing.T) {
	t.Parallel()

	for kind, res := range ownershipResources() {
		t.Run(kind+" owned is deleted", func(t *testing.T) {
			t.Parallel()

			var deletes int32

			srv := newOwnershipServer(t, res, ownedLabels, false, &deletes)
			prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

			require.NoError(t, res.remove(prov, t.Context()))
			assert.Equal(t, int32(1), atomic.LoadInt32(&deletes))
		})

		for variant, labels := range map[string]string{
			"foreign":              foreignLabels,
			"other cluster":        otherCluster,
			"missing cluster name": noClusterLabel,
		} {
			t.Run(kind+" "+variant+" is kept and reported", func(t *testing.T) {
				t.Parallel()

				var deletes int32

				srv := newOwnershipServer(t, res, labels, false, &deletes)
				prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

				var out bytes.Buffer
				prov.SetLogWriter(&out)

				require.NoError(t, res.remove(prov, t.Context()))
				assert.Zero(t, atomic.LoadInt32(&deletes),
					"a resource not owned by this cluster must survive")
				assert.Contains(t, out.String(), "Keeping "+kind)
				assert.Contains(t, out.String(), `KSail cluster "test-cluster"`)
			})
		}

		t.Run(kind+" lookup error fails", func(t *testing.T) {
			t.Parallel()

			var deletes int32

			srv := newOwnershipServer(t, res, ownedLabels, true, &deletes)
			prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

			err := res.remove(prov, t.Context())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "insufficient permissions")
			assert.Zero(t, atomic.LoadInt32(&deletes))
		})
	}
}

// TestDeleteLoadBalancers_OnlyRemovesClusterLoadBalancers proves that a load
// balancer is deleted only when it sits on this cluster's KSail-owned network
// and was created by hcloud-ccm or carries this cluster's KSail labels. Any
// other load balancer on the network is kept and reported.
func TestDeleteLoadBalancers_OnlyRemovesClusterLoadBalancers(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		network, lb string
		deleted     bool
		reported    bool
	}{
		"ccm lb on owned network":        {ownedLabels, ccmLBLabels, true, false},
		"ksail lb on owned network":      {ownedLabels, ownedLabels, true, false},
		"unlabelled lb on owned network": {ownedLabels, `{}`, false, true},
		"other cluster lb":               {ownedLabels, otherCluster, false, true},
		"ccm lb on foreign network":      {foreignLabels, ccmLBLabels, false, false},
		"ccm lb on other cluster net":    {otherCluster, ccmLBLabels, false, false},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var deletes atomic.Int32

			srv := newLoadBalancerOwnershipServer(t, testCase.network, testCase.lb, &deletes, nil)

			prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

			var out bytes.Buffer
			prov.SetLogWriter(&out)

			require.NoError(t, prov.DeleteLoadBalancersForTest(t.Context(), ownershipCluster))

			want := int32(0)
			if testCase.deleted {
				want = 1
			}

			assert.Equal(t, want, deletes.Load())

			if testCase.reported {
				assert.Contains(t, out.String(), `Keeping load balancer "svc-lb"`)
			} else {
				assert.Empty(t, out.String())
			}
		})
	}
}

// newLoadBalancerOwnershipServer serves one network with networkLabels and one
// load balancer attached to it with lbLabels, and counts load balancer deletes
// and, when networkDeletes is set, network deletes.
func newLoadBalancerOwnershipServer(
	t *testing.T,
	networkLabels, lbLabels string,
	deletes, networkDeletes *atomic.Int32,
) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/networks", func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Content-Type", "application/json")
		_, _ = responseWriter.Write([]byte(`{"networks":[` +
			ownershipResources()["network"].item(networkLabels) + `]}`))
	})
	mux.HandleFunc("/networks/11", func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodDelete && networkDeletes != nil {
			networkDeletes.Add(1)
		}

		responseWriter.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc(
		"/load_balancers",
		func(responseWriter http.ResponseWriter, _ *http.Request) {
			responseWriter.Header().Set("Content-Type", "application/json")
			_, _ = responseWriter.Write(
				[]byte(`{"load_balancers":[{"id":7,"name":"svc-lb",` +
					`"labels":` + lbLabels + `,` +
					`"private_net":[{"network":11,"ip":"10.0.0.5"}]}],` +
					`"meta":{"pagination":{"page":1,"per_page":50,"total_entries":1}}}`),
			)
		},
	)
	mux.HandleFunc(
		"/load_balancers/7",
		func(responseWriter http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodDelete {
				deletes.Add(1)
			}

			responseWriter.Header().Set("Content-Type", "application/json")
			_, _ = responseWriter.Write([]byte(loadBalancerDeleteReply))
		},
	)
	mux.HandleFunc("/actions", func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Content-Type", "application/json")
		_, _ = responseWriter.Write([]byte(
			`{"actions":[{"id":9,"command":"delete_load_balancer","status":"success"}]}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

// TestDeleteLoadBalancersAndNetwork_KeepsNetworkWithForeignLoadBalancer proves
// that the cluster's network is not deleted while a load balancer KSail keeps
// is still attached to it: deleting the network would detach that load balancer.
func TestDeleteLoadBalancersAndNetwork_KeepsNetworkWithForeignLoadBalancer(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		lb             string
		networkDeleted bool
	}{
		"owned lb removed, network deleted": {ccmLBLabels, true},
		"foreign lb kept, network kept":     {`{}`, false},
		"other cluster lb kept, net kept":   {otherCluster, false},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var lbDeletes, networkDeletes atomic.Int32

			srv := newLoadBalancerOwnershipServer(
				t, ownedLabels, testCase.lb, &lbDeletes, &networkDeletes,
			)
			prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

			var out bytes.Buffer
			prov.SetLogWriter(&out)

			require.NoError(
				t,
				prov.DeleteLoadBalancersAndNetworkForTest(t.Context(), ownershipCluster),
			)

			if testCase.networkDeleted {
				assert.Equal(t, int32(1), networkDeletes.Load())
				assert.NotContains(t, out.String(), "Keeping network")

				return
			}

			assert.Zero(t, lbDeletes.Load())
			assert.Zero(t, networkDeletes.Load(),
				"the network must survive while a kept load balancer is attached")
			assert.Contains(t, out.String(), `Keeping network "test-cluster-network"`)
			assert.Contains(t, out.String(), "svc-lb")
		})
	}
}
