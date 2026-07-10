package hetzner_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	hcloudtest "github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ownedFloatingIPJSON is the canned API representation of the cluster's
// ksail-owned floating IP (id 7, name "test-cluster-floating-ip").
const ownedFloatingIPJSON = `{"id":7,"name":"test-cluster-floating-ip","description":"",` +
	`"ip":"192.0.2.10","type":"ipv4","server":null,"dns_ptr":[],` +
	`"home_location":{"id":1,"name":"fsn1","description":"","country":"DE","city":"",` +
	`"latitude":0,"longitude":0,"network_zone":"eu-central"},` +
	`"blocked":false,"protection":{"delete":false},` +
	`"labels":{"ksail.owned":"true","ksail.cluster.name":"test-cluster"},` +
	`"created":"2026-07-02T00:00:00+00:00"}`

// unownedFloatingIPJSON is the same address without the ksail.owned label — a
// user-managed reserved address that ksail must neither adopt nor release.
const unownedFloatingIPJSON = `{"id":7,"name":"test-cluster-floating-ip","description":"",` +
	`"ip":"192.0.2.10","type":"ipv4","server":null,"dns_ptr":[],` +
	`"home_location":{"id":1,"name":"fsn1","description":"","country":"DE","city":"",` +
	`"latitude":0,"longitude":0,"network_zone":"eu-central"},` +
	`"blocked":false,"protection":{"delete":false},"labels":{},` +
	`"created":"2026-07-02T00:00:00+00:00"}`

// newFloatingIPServer returns an httptest server answering the floating IP
// list (GetByName) with listJSON, counting Create/Delete/Assign/Unassign
// calls, and storing the last Create request body.
func newFloatingIPServer(
	t *testing.T,
	listJSON string,
	createCalls, deleteCalls, assignCalls, unassignCalls *int32,
	lastCreateBody *atomic.Value,
) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc(
		"/floating_ips",
		func(responseWriter http.ResponseWriter, request *http.Request) {
			responseWriter.Header().Set("Content-Type", "application/json")

			if request.Method == http.MethodPost {
				atomic.AddInt32(createCalls, 1)

				body, _ := io.ReadAll(request.Body)
				lastCreateBody.Store(string(body))

				_, _ = responseWriter.Write([]byte(
					`{"floating_ip":` + ownedFloatingIPJSON + `,"action":null}`))

				return
			}

			_, _ = responseWriter.Write([]byte(`{"floating_ips":[` + listJSON + `]}`))
		},
	)

	mux.HandleFunc(
		"/floating_ips/7",
		func(responseWriter http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodDelete {
				atomic.AddInt32(deleteCalls, 1)
				responseWriter.WriteHeader(http.StatusNoContent)
			}
		},
	)

	registerFloatingIPActionHandlers(mux, assignCalls, unassignCalls)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

// registerFloatingIPActionHandlers wires the assign/unassign action endpoints
// onto the fake floating IP server, counting each invocation.
func registerFloatingIPActionHandlers(mux *http.ServeMux, assignCalls, unassignCalls *int32) {
	actionResponse := func(responseWriter http.ResponseWriter, command string) {
		responseWriter.Header().Set("Content-Type", "application/json")
		_, _ = responseWriter.Write([]byte(
			`{"action":{"id":1,"command":"` + command + `","status":"success","progress":100}}`))
	}

	mux.HandleFunc("/floating_ips/7/actions/assign",
		func(responseWriter http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(assignCalls, 1)
			actionResponse(responseWriter, "assign_floating_ip")
		})

	mux.HandleFunc("/floating_ips/7/actions/unassign",
		func(responseWriter http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(unassignCalls, 1)
			actionResponse(responseWriter, "unassign_floating_ip")
		})
}

// floatingIPCounters bundles the per-test call counters.
type floatingIPCounters struct {
	create, del, assign, unassign int32
	lastCreateBody                atomic.Value
}

func newFloatingIPProvider(
	t *testing.T,
	listJSON string,
) (*hetzner.Provider, *floatingIPCounters) {
	t.Helper()

	counters := &floatingIPCounters{}
	srv := newFloatingIPServer(
		t, listJSON,
		&counters.create, &counters.del, &counters.assign, &counters.unassign,
		&counters.lastCreateBody,
	)

	return hetzner.NewProvider(newTestHcloudClient(srv.URL)), counters
}

func TestEnsureFloatingIP_CreatesWhenAbsent(t *testing.T) {
	t.Parallel()

	prov, counters := newFloatingIPProvider(t, "")

	floatingIP, err := prov.EnsureFloatingIP(t.Context(), "test-cluster", "fsn1")

	require.NoError(t, err)
	require.NotNil(t, floatingIP)
	assert.Equal(t, int64(7), floatingIP.ID)
	assert.Equal(t, int32(1), atomic.LoadInt32(&counters.create))

	// The create request carries the conventional name, IPv4 type, home
	// location, and ksail ownership labels. Decoded as a map because the
	// request body follows the Hetzner API's snake_case schema.
	var createBody map[string]any

	body, _ := counters.lastCreateBody.Load().(string)
	require.NoError(t, json.Unmarshal([]byte(body), &createBody))
	assert.Equal(t, "test-cluster-floating-ip", createBody["name"])
	assert.Equal(t, "ipv4", createBody["type"])
	assert.Equal(t, "fsn1", createBody["home_location"])

	labels, ok := createBody["labels"].(map[string]any)
	require.True(t, ok, "create body labels should be an object")
	assert.Equal(t, "true", labels["ksail.owned"])
	assert.Equal(t, "test-cluster", labels["ksail.cluster.name"])
}

func TestEnsureFloatingIP_ReturnsExistingWithoutCreate(t *testing.T) {
	t.Parallel()

	prov, counters := newFloatingIPProvider(t, ownedFloatingIPJSON)

	floatingIP, err := prov.EnsureFloatingIP(t.Context(), "test-cluster", "fsn1")

	require.NoError(t, err)
	require.NotNil(t, floatingIP)
	assert.Equal(t, int64(7), floatingIP.ID)
	assert.Equal(t, int32(0), atomic.LoadInt32(&counters.create))
}

func TestEnsureFloatingIP_RejectsUnownedNameCollision(t *testing.T) {
	t.Parallel()

	// A user-managed reserved address sharing the conventional name must not
	// be silently adopted for the cluster (and a same-name create would fail
	// Hetzner's name uniqueness) — surface the collision as an error instead.
	prov, counters := newFloatingIPProvider(t, unownedFloatingIPJSON)

	floatingIP, err := prov.EnsureFloatingIP(t.Context(), "test-cluster", "fsn1")

	require.ErrorIs(t, err, hetzner.ErrFloatingIPNotOwned)
	assert.Nil(t, floatingIP)
	assert.Equal(t, int32(0), atomic.LoadInt32(&counters.create))
}

func TestEnsureFloatingIP_NilClient(t *testing.T) {
	t.Parallel()

	prov := hetzner.NewProvider(nil)

	_, err := prov.EnsureFloatingIP(t.Context(), "test-cluster", "fsn1")

	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestOwnedFloatingIPExists_FalseWhenAbsent(t *testing.T) {
	t.Parallel()

	prov, counters := newFloatingIPProvider(t, "")

	exists, err := prov.OwnedFloatingIPExists(t.Context(), "test-cluster")

	require.NoError(t, err)
	assert.False(t, exists)
	assert.Equal(t, int32(0), atomic.LoadInt32(&counters.create),
		"the read-only lookup must never create")
}

func TestOwnedFloatingIPExists_TrueWhenOwned(t *testing.T) {
	t.Parallel()

	prov, counters := newFloatingIPProvider(t, ownedFloatingIPJSON)

	exists, err := prov.OwnedFloatingIPExists(t.Context(), "test-cluster")

	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, int32(0), atomic.LoadInt32(&counters.create),
		"the read-only lookup must never create")
}

func TestOwnedFloatingIPExists_RejectsUnownedNameCollision(t *testing.T) {
	t.Parallel()

	// Same ownership guard as EnsureFloatingIP: a user-managed reserved address
	// sharing the conventional name is not the cluster's floating IP, and
	// counting it as present would mask the collision until apply time.
	prov, _ := newFloatingIPProvider(t, unownedFloatingIPJSON)

	exists, err := prov.OwnedFloatingIPExists(t.Context(), "test-cluster")

	require.ErrorIs(t, err, hetzner.ErrFloatingIPNotOwned)
	assert.False(t, exists)
}

func TestOwnedFloatingIPExists_NilClient(t *testing.T) {
	t.Parallel()

	prov := hetzner.NewProvider(nil)

	_, err := prov.OwnedFloatingIPExists(t.Context(), "test-cluster")

	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestAttachFloatingIPToServer_NoopWhenAlreadyAssigned(t *testing.T) {
	t.Parallel()

	prov, counters := newFloatingIPProvider(t, "")
	floatingIP := &hcloudtest.FloatingIP{
		ID:     7,
		Server: &hcloudtest.Server{ID: 9},
	}

	err := prov.AttachFloatingIPToServer(t.Context(), floatingIP, &hcloudtest.Server{ID: 9})

	require.NoError(t, err)
	assert.Equal(t, int32(0), atomic.LoadInt32(&counters.assign))
}

func TestAttachFloatingIPToServer_AssignsWhenUnassigned(t *testing.T) {
	t.Parallel()

	prov, counters := newFloatingIPProvider(t, "")

	err := prov.AttachFloatingIPToServer(
		t.Context(),
		&hcloudtest.FloatingIP{ID: 7},
		&hcloudtest.Server{ID: 9},
	)

	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&counters.assign))
}

func TestAttachFloatingIPToServer_ReassignsWhenAssignedElsewhere(t *testing.T) {
	t.Parallel()

	prov, counters := newFloatingIPProvider(t, "")
	floatingIP := &hcloudtest.FloatingIP{
		ID:     7,
		Server: &hcloudtest.Server{ID: 3},
	}

	err := prov.AttachFloatingIPToServer(t.Context(), floatingIP, &hcloudtest.Server{ID: 9})

	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&counters.assign))
}

func TestDetachFloatingIP_NoopWhenUnassigned(t *testing.T) {
	t.Parallel()

	prov, counters := newFloatingIPProvider(t, "")

	err := prov.DetachFloatingIP(t.Context(), &hcloudtest.FloatingIP{ID: 7})

	require.NoError(t, err)
	assert.Equal(t, int32(0), atomic.LoadInt32(&counters.unassign))
}

func TestDetachFloatingIP_UnassignsWhenAssigned(t *testing.T) {
	t.Parallel()

	prov, counters := newFloatingIPProvider(t, "")
	floatingIP := &hcloudtest.FloatingIP{
		ID:     7,
		Server: &hcloudtest.Server{ID: 9},
	}

	err := prov.DetachFloatingIP(t.Context(), floatingIP)

	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&counters.unassign))
}

func TestDeleteFloatingIP_DeletesOwned(t *testing.T) {
	t.Parallel()

	prov, counters := newFloatingIPProvider(t, ownedFloatingIPJSON)

	err := prov.DeleteFloatingIPForTest(t.Context(), "test-cluster")

	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&counters.del))
}

func TestDeleteFloatingIP_SkipsUnowned(t *testing.T) {
	t.Parallel()

	prov, counters := newFloatingIPProvider(t, unownedFloatingIPJSON)

	err := prov.DeleteFloatingIPForTest(t.Context(), "test-cluster")

	require.NoError(t, err)
	assert.Equal(t, int32(0), atomic.LoadInt32(&counters.del))
}

func TestDeleteFloatingIP_NoopWhenAbsent(t *testing.T) {
	t.Parallel()

	prov, counters := newFloatingIPProvider(t, "")

	err := prov.DeleteFloatingIPForTest(t.Context(), "test-cluster")

	require.NoError(t, err)
	assert.Equal(t, int32(0), atomic.LoadInt32(&counters.del))
}

func TestDeleteFloatingIP_PropagatesLookupError(t *testing.T) {
	t.Parallel()

	// A real API failure (here a non-retryable 401) must propagate — treating
	// it as "not found" would silently skip the release and leak a billed
	// reserved address. GetByName reports absence as nil/nil, never an error.
	var deleteCalls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc(
		"/floating_ips",
		func(responseWriter http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodDelete {
				deleteCalls.Add(1)
			}

			responseWriter.Header().Set("Content-Type", "application/json")
			responseWriter.WriteHeader(http.StatusUnauthorized)
			_, _ = responseWriter.Write([]byte(
				`{"error":{"code":"unauthorized","message":"unable to authenticate"}}`))
		},
	)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

	err := prov.DeleteFloatingIPForTest(t.Context(), "test-cluster")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get floating IP test-cluster-floating-ip")
	assert.Equal(t, int32(0), deleteCalls.Load())
}
