package hetzner_test

import (
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

const stopTestCluster = "stop-test"

// stopTestAPI is a fake Hetzner API holding the power state of a cluster's
// servers. Servers whose name is in ignoreShutdown stay running after a graceful
// shutdown, like a node whose operating system never finishes shutting down.
type stopTestAPI struct {
	mu             sync.Mutex
	status         map[string]string
	ids            map[int64]string
	ignoreShutdown map[string]bool
	shutdowns      []string
	poweroffs      []string
}

func newStopTestAPI(status map[string]string, ignoreShutdown ...string) *stopTestAPI {
	api := &stopTestAPI{
		status:         status,
		ids:            map[int64]string{},
		ignoreShutdown: map[string]bool{},
	}

	var nextID int64 = 1
	for name := range status {
		api.ids[nextID] = name
		nextID++
	}

	for _, name := range ignoreShutdown {
		api.ignoreShutdown[name] = true
	}

	return api
}

func (api *stopTestAPI) servers(nameFilter string) []schema.Server {
	api.mu.Lock()
	defer api.mu.Unlock()

	servers := []schema.Server{}

	for serverID, name := range api.ids {
		if nameFilter != "" && nameFilter != name {
			continue
		}

		servers = append(servers, schema.Server{
			ID:     serverID,
			Name:   name,
			Status: api.status[name],
			Labels: map[string]string{
				hetzner.LabelOwned:       "true",
				hetzner.LabelClusterName: stopTestCluster,
			},
		})
	}

	return servers
}

func (api *stopTestAPI) act(t *testing.T, writer http.ResponseWriter, request *http.Request) {
	t.Helper()

	serverID, err := strconv.ParseInt(request.PathValue("id"), 10, 64)
	if err != nil {
		t.Errorf("parse id: %v", err)
		http.Error(writer, err.Error(), http.StatusBadRequest)

		return
	}

	command := request.PathValue("command")

	api.mu.Lock()

	name := api.ids[serverID]

	switch command {
	case "shutdown":
		api.shutdowns = append(api.shutdowns, name)
		if !api.ignoreShutdown[name] {
			api.status[name] = "off"
		}
	case "poweroff":
		api.poweroffs = append(api.poweroffs, name)
		api.status[name] = "off"
	default:
		t.Errorf("unexpected server action %q", command)
	}

	api.mu.Unlock()

	writeJSONResponse(t, writer, map[string]any{"action": successTestAction(serverID)})
}

func (api *stopTestAPI) start(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /servers", func(writer http.ResponseWriter, request *http.Request) {
		writeJSONResponse(
			t,
			writer,
			schema.ServerListResponse{Servers: api.servers(request.URL.Query().Get("name"))},
		)
	})

	mux.HandleFunc(
		"POST /servers/{id}/actions/{command}",
		func(writer http.ResponseWriter, request *http.Request) {
			api.act(t, writer, request)
		},
	)

	mux.HandleFunc("GET /actions", func(writer http.ResponseWriter, request *http.Request) {
		actionID, err := strconv.ParseInt(request.URL.Query().Get("id"), 10, 64)
		if err != nil {
			t.Errorf("parse id: %v", err)
			http.Error(writer, err.Error(), http.StatusBadRequest)

			return
		}

		writeJSONResponse(t, writer, map[string]any{"actions": []any{successTestAction(actionID)}})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

// TestStopNodes_GracefulShutdownNeedsNoPowerOff proves that servers which shut
// down gracefully are never forced off.
func TestStopNodes_GracefulShutdownNeedsNoPowerOff(t *testing.T) {
	t.Parallel()

	api := newStopTestAPI(map[string]string{"cp-1": "running", "worker-1": "running"})
	prov := hetzner.NewProvider(newTestHcloudClient(t, api.start(t).URL))
	prov.SetGracefulShutdownTimeoutForTest(hetzner.DefaultPollingInterval + 3*time.Second)

	require.NoError(t, prov.StopNodes(t.Context(), stopTestCluster))

	assert.ElementsMatch(t, []string{"cp-1", "worker-1"}, api.shutdowns)
	assert.Empty(t, api.poweroffs, "a graceful shutdown that finished must not be forced")
}

// TestStopNodes_PowersOffServersThatIgnoreShutdown proves that a server still
// running after the graceful window is powered off, so the stop succeeds
// instead of timing out, and that only that server is forced.
func TestStopNodes_PowersOffServersThatIgnoreShutdown(t *testing.T) {
	t.Parallel()

	api := newStopTestAPI(
		map[string]string{"cp-1": "running", "worker-1": "running", "worker-2": "off"},
		"cp-1",
	)
	prov := hetzner.NewProvider(newTestHcloudClient(t, api.start(t).URL))
	prov.SetGracefulShutdownTimeoutForTest(hetzner.DefaultPollingInterval + time.Second)

	require.NoError(t, prov.StopNodes(t.Context(), stopTestCluster))

	assert.ElementsMatch(t, []string{"cp-1", "worker-1"}, api.shutdowns,
		"a server that is already off is not shut down again")
	assert.Equal(t, []string{"cp-1"}, api.poweroffs,
		"only the server still running after the graceful window is forced off")
}
