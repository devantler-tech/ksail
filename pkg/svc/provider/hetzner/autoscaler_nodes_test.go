package hetzner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/hetznercloud/hcloud-go/v2/hcloud/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	autoscalerTestCluster = "my-cluster"
	autoscalerTestNetwork = "my-cluster-network"
	autoscalerNetworkID   = int64(100)
	autoscalerPoolA       = "pool-a"
	autoscalerPoolB       = "pool-b"
)

// schemaServer builds a minimal server schema attached to the given network ID.
func schemaServer(id int64, name string, networkID int64) schema.Server {
	return schema.Server{
		ID:     id,
		Name:   name,
		Status: "running",
		PrivateNet: []schema.ServerPrivateNet{
			{Network: networkID, IP: "10.0.0.2"},
		},
	}
}

// poolSelector is the label selector that matches the servers of one node-group pool.
func poolSelector(pool string) string {
	return hetzner.LabelAutoscalerNodeGroup + "=" + pool
}

// newAutoscalerNodesTestServer mocks the Hetzner network-lookup and server-list
// endpoints ListAutoscalerNodes depends on. serversByPool maps a node-group pool
// name to the servers returned for its label selector.
func newAutoscalerNodesTestServer(
	t *testing.T,
	serversByPool map[string][]schema.Server,
) *httptest.Server {
	t.Helper()

	serversBySelector := make(map[string][]schema.Server, len(serversByPool))
	for pool, servers := range serversByPool {
		serversBySelector[poolSelector(pool)] = servers
	}

	return newAutoscalerSelectorTestServer(t, serversBySelector, &selectorLog{})
}

// selectorLog records the label selectors the provider sends, safe for the test
// server's handler goroutines.
type selectorLog struct {
	mu        sync.Mutex
	selectors []string
}

func (l *selectorLog) add(selector string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.selectors = append(l.selectors, selector)
}

func (l *selectorLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.selectors)
}

// newAutoscalerSelectorTestServer mocks the Hetzner network-lookup and server-list
// endpoints. serversBySelector maps an exact label selector to the servers returned
// for it; every selector the provider sends is recorded on selectors.
func newAutoscalerSelectorTestServer(
	t *testing.T,
	serversBySelector map[string][]schema.Server,
	selectors *selectorLog,
) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /networks", func(writer http.ResponseWriter, request *http.Request) {
		resp := schema.NetworkListResponse{}
		if request.URL.Query().Get("name") == autoscalerTestNetwork {
			resp.Networks = []schema.Network{{ID: autoscalerNetworkID, Name: autoscalerTestNetwork}}
		}

		writeJSONResponse(t, writer, resp)
	})

	mux.HandleFunc("GET /servers", func(writer http.ResponseWriter, request *http.Request) {
		selector := request.URL.Query().Get("label_selector")
		selectors.add(selector)

		writeJSONResponse(
			t,
			writer,
			schema.ServerListResponse{Servers: serversBySelector[selector]},
		)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func TestListAutoscalerNodes_EmptyPoolNames(t *testing.T) {
	t.Parallel()

	client := hcloud.NewClient(hcloud.WithToken("test-token"))
	prov := hetzner.NewProvider(client)

	servers, err := prov.ListAutoscalerNodes(context.Background(), autoscalerTestCluster, nil)

	require.NoError(t, err)
	assert.Empty(t, servers)
}

func TestListAutoscalerNodes_NilClient(t *testing.T) {
	t.Parallel()

	prov := hetzner.NewProvider(nil)

	_, err := prov.ListAutoscalerNodes(
		context.Background(), autoscalerTestCluster, []string{autoscalerPoolA},
	)

	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestListAutoscalerNodes_MissingNetworkReturnsNothing(t *testing.T) {
	t.Parallel()

	// No network matches the cluster name, so there is nothing to recycle.
	srv := newAutoscalerNodesTestServer(t, map[string][]schema.Server{
		autoscalerPoolA: {schemaServer(1, "as-1", autoscalerNetworkID)},
	})
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	servers, err := prov.ListAutoscalerNodes(
		context.Background(), "absent-cluster", []string{autoscalerPoolA},
	)

	require.NoError(t, err)
	assert.Empty(t, servers)
}

func TestListAutoscalerNodes_FiltersByNetworkAndDedupes(t *testing.T) {
	t.Parallel()

	const otherNetworkID = int64(999)

	srv := newAutoscalerNodesTestServer(t, map[string][]schema.Server{
		autoscalerPoolA: {
			schemaServer(1, "as-1", autoscalerNetworkID),
			schemaServer(2, "as-2", otherNetworkID), // different network → excluded
			schemaServer(3, "as-3", autoscalerNetworkID),
		},
		autoscalerPoolB: {
			schemaServer(3, "as-3", autoscalerNetworkID), // duplicate across pools → once
			schemaServer(4, "as-4", autoscalerNetworkID),
		},
	})
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	servers, err := prov.ListAutoscalerNodes(
		context.Background(), autoscalerTestCluster, []string{autoscalerPoolA, autoscalerPoolB},
	)

	require.NoError(t, err)

	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.Name)
	}

	assert.ElementsMatch(t, []string{"as-1", "as-3", "as-4"}, names)
}

// inPool labels a server schema with the node-group pool the cluster autoscaler
// created it for.
func inPool(server schema.Server, pool string) schema.Server {
	server.Labels = map[string]string{hetzner.LabelAutoscalerNodeGroup: pool}

	return server
}

// TestListClusterAutoscalerNodes_FindsEveryPoolInClusterNetwork pins #7327: the
// cluster-wide listing selects on the node-group label key alone, so a server of a
// pool that is no longer configured is found, while a server of another cluster's
// network stays out even when it uses the same pool name.
func TestListClusterAutoscalerNodes_FindsEveryPoolInClusterNetwork(t *testing.T) {
	t.Parallel()

	const otherNetworkID = int64(999)

	selectors := &selectorLog{}
	srv := newAutoscalerSelectorTestServer(t, map[string][]schema.Server{
		hetzner.LabelAutoscalerNodeGroup: {
			inPool(schemaServer(1, "as-pool-a", autoscalerNetworkID), autoscalerPoolA),
			inPool(schemaServer(2, "as-removed-pool", autoscalerNetworkID), "removed-pool"),
			inPool(schemaServer(3, "as-other-cluster", otherNetworkID), autoscalerPoolA),
		},
	}, selectors)
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	servers, err := prov.ListClusterAutoscalerNodes(context.Background(), autoscalerTestCluster)

	require.NoError(t, err)

	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.Name)
	}

	assert.ElementsMatch(t, []string{"as-pool-a", "as-removed-pool"}, names)
	assert.Equal(t, []string{hetzner.LabelAutoscalerNodeGroup}, selectors.all(),
		"the listing must select on the label key, not on configured pool names")
}

func TestListClusterAutoscalerNodes_MissingNetworkReturnsNothing(t *testing.T) {
	t.Parallel()

	srv := newAutoscalerSelectorTestServer(t, map[string][]schema.Server{
		hetzner.LabelAutoscalerNodeGroup: {
			inPool(schemaServer(1, "as-1", autoscalerNetworkID), autoscalerPoolA),
		},
	}, &selectorLog{})
	prov := hetzner.NewProvider(newTestHcloudClient(t, srv.URL))

	servers, err := prov.ListClusterAutoscalerNodes(context.Background(), "absent-cluster")

	require.NoError(t, err)
	assert.Empty(t, servers)
}

func TestListClusterAutoscalerNodes_NilClient(t *testing.T) {
	t.Parallel()

	prov := hetzner.NewProvider(nil)

	_, err := prov.ListClusterAutoscalerNodes(context.Background(), autoscalerTestCluster)

	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestServerInNetwork(t *testing.T) {
	t.Parallel()

	server := func(networkIDs ...int64) *hcloud.Server {
		nets := make([]hcloud.ServerPrivateNet, 0, len(networkIDs))
		for _, id := range networkIDs {
			nets = append(nets, hcloud.ServerPrivateNet{Network: &hcloud.Network{ID: id}})
		}

		return &hcloud.Server{PrivateNet: nets}
	}

	tests := []struct {
		name      string
		server    *hcloud.Server
		networkID int64
		want      bool
	}{
		{"matching network", server(100), 100, true},
		{"different network", server(200), 100, false},
		{"no private networks", server(), 100, false},
		{"multiple, one matching", server(200, 100), 100, true},
		{"nil network field", &hcloud.Server{
			PrivateNet: []hcloud.ServerPrivateNet{{Network: nil}},
		}, 100, false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := hetzner.ServerInNetworkForTest(testCase.server, testCase.networkID)
			assert.Equal(t, testCase.want, got)
		})
	}
}
