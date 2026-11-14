package hetzner_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
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
	// stallStatusRead, when set, holds the first server list that follows the
	// graceful shutdowns open until the client gives up on it, like a slow API
	// whose status read is still in flight when the graceful window ends.
	stallStatusRead bool
	stalled         bool
}

// stallsThisRead reports whether a server list request must be held open.
func (api *stopTestAPI) stallsThisRead(nameFilter string) bool {
	api.mu.Lock()
	defer api.mu.Unlock()

	if !api.stallStatusRead || api.stalled || nameFilter != "" || len(api.shutdowns) == 0 {
		return false
	}

	api.stalled = true

	return true
}

// recorded returns the servers shut down, the servers powered off, and whether
// the API held a status read open, all read under the lock its handlers write
// them under. A stalled handler can still be running after the client gave up
// on it, so a test must never read these fields directly.
func (api *stopTestAPI) recorded() ([]string, []string, bool) {
	api.mu.Lock()
	defer api.mu.Unlock()

	return slices.Clone(api.shutdowns), slices.Clone(api.poweroffs), api.stalled
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
		if api.stallsThisRead(request.URL.Query().Get("name")) {
			<-request.Context().Done()

			return
		}

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

	shutdowns, poweroffs, _ := api.recorded()

	assert.ElementsMatch(t, []string{"cp-1", "worker-1"}, shutdowns)
	assert.Empty(t, poweroffs, "a graceful shutdown that finished must not be forced")
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

	shutdowns, poweroffs, _ := api.recorded()

	assert.ElementsMatch(t, []string{"cp-1", "worker-1"}, shutdowns,
		"a server that is already off is not shut down again")
	assert.Equal(t, []string{"cp-1"}, poweroffs,
		"only the server still running after the graceful window is forced off")
}

// TestStopNodes_PowersOffWhenTheLastStatusReadFailsAtTheWindowDeadline proves
// that a status read still in flight when the graceful window ends does not
// stop the power off. The provider cannot tell a read its own deadline
// interrupted from one that failed with a matching error at that moment, and
// in both cases the window has run out. The power off step lists the servers
// again, so it acts on their current state and fails if the API is down.
func TestStopNodes_PowersOffWhenTheLastStatusReadFailsAtTheWindowDeadline(t *testing.T) {
	t.Parallel()

	api := newStopTestAPI(map[string]string{"cp-1": "running", "worker-1": "running"}, "cp-1")
	api.stallStatusRead = true
	prov := hetzner.NewProvider(newTestHcloudClient(t, api.start(t).URL))
	prov.SetGracefulShutdownTimeoutForTest(hetzner.DefaultPollingInterval + time.Second)

	require.NoError(t, prov.StopNodes(t.Context(), stopTestCluster))

	_, poweroffs, stalled := api.recorded()

	assert.True(t, stalled, "the status read at the window deadline must have been interrupted")
	assert.Equal(t, []string{"cp-1"}, poweroffs,
		"only the server still running after the graceful window is forced off")
}
