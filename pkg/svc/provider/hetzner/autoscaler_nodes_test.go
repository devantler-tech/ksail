package hetzner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
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

// newAutoscalerNodesTestServer mocks the Hetzner network-lookup and server-list
// endpoints ListAutoscalerNodes depends on. serversByPool maps a node-group pool
// name to the servers returned for its label selector.
func newAutoscalerNodesTestServer(
	t *testing.T,
	serversByPool map[string][]schema.Server,
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

		resp := schema.ServerListResponse{}

		for pool, servers := range serversByPool {
			if selector == hetzner.LabelAutoscalerNodeGroup+"="+pool {
				resp.Servers = servers

				break
			}
		}

		writeJSONResponse(t, writer, resp)
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
	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

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
	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

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
