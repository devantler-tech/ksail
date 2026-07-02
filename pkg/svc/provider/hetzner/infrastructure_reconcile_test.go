package hetzner_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newNetworkReconcileServer returns an httptest server that answers the network
// GetByName lookup with a single existing network carrying the given ip_range and
// subnets, records how many times AddSubnet is invoked, and stores the last
// AddSubnet request body. It lets the reconcile tests assert whether EnsureNetwork
// adds the server subnet — and which subnet — when adopting an existing network.
func newNetworkReconcileServer(
	t *testing.T,
	networkCIDR string,
	subnetsJSON string,
	addSubnetCalls *int32,
	lastAddSubnetBody *atomic.Value,
) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// GetByName lists networks filtered by name -> return one existing network.
	mux.HandleFunc("/networks", func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Content-Type", "application/json")
		_, _ = responseWriter.Write([]byte(`{"networks":[{"id":42,"name":"test-cluster-network",` +
			`"ip_range":"` + networkCIDR + `","subnets":` + subnetsJSON + `,"servers":[],"labels":{}}]}`))
	})

	// AddSubnet posts an action against the network id.
	mux.HandleFunc("/networks/42/actions/add_subnet",
		func(responseWriter http.ResponseWriter, request *http.Request) {
			atomic.AddInt32(addSubnetCalls, 1)

			body, _ := io.ReadAll(request.Body)
			lastAddSubnetBody.Store(string(body))

			responseWriter.Header().Set("Content-Type", "application/json")
			_, _ = responseWriter.Write([]byte(
				`{"action":{"id":1,"command":"add_subnet","status":"success","progress":100}}`))
		})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

// TestEnsureNetwork_ReconcilesMissingSubnetOnExisting proves that when a prior
// Create left the network created but subnet-less (AddSubnet failed), a later
// EnsureNetwork call adopts the existing network AND adds the missing subnet,
// healing the partial state instead of returning a subnet-less network.
func TestEnsureNetwork_ReconcilesMissingSubnetOnExisting(t *testing.T) {
	t.Parallel()

	var (
		addSubnetCalls    int32
		lastAddSubnetBody atomic.Value
	)

	srv := newNetworkReconcileServer(
		t,
		defaultNetworkCIDR,
		`[]`,
		&addSubnetCalls,
		&lastAddSubnetBody,
	)
	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

	network, err := prov.EnsureNetwork(context.Background(), "test-cluster", defaultNetworkCIDR)

	require.NoError(t, err)
	require.NotNil(t, network)
	assert.Equal(t, int32(1), atomic.LoadInt32(&addSubnetCalls),
		"EnsureNetwork should add the missing subnet when adopting an existing subnet-less network")
	assert.Contains(t, lastAddSubnetBody.Load(), `"ip_range":"10.0.1.0/24"`,
		"the default network keeps its historical 10.0.1.0/24 server subnet")
}

// TestEnsureNetwork_SkipsSubnetWhenPresent proves the reconcile is idempotent:
// when the existing network already carries the expected server subnet, no
// duplicate AddSubnet call is made.
func TestEnsureNetwork_SkipsSubnetWhenPresent(t *testing.T) {
	t.Parallel()

	var (
		addSubnetCalls    int32
		lastAddSubnetBody atomic.Value
	)

	existingSubnet := `[{"type":"cloud","ip_range":"10.0.1.0/24","network_zone":"eu-central","gateway":"10.0.0.1"}]`
	srv := newNetworkReconcileServer(
		t, defaultNetworkCIDR, existingSubnet, &addSubnetCalls, &lastAddSubnetBody,
	)
	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

	network, err := prov.EnsureNetwork(context.Background(), "test-cluster", defaultNetworkCIDR)

	require.NoError(t, err)
	require.NotNil(t, network)
	assert.Equal(t, int32(0), atomic.LoadInt32(&addSubnetCalls),
		"EnsureNetwork should not re-add a subnet the network already carries")
}

// TestEnsureNetwork_CustomCIDRAddsFirstSlash24 proves a custom network range gets
// the FIRST /24 of that range as its server subnet — not the whole range, which
// would break the reconcile (a subnet equal to the network leaves no room and
// mismatches what the comment and provisioners expect).
func TestEnsureNetwork_CustomCIDRAddsFirstSlash24(t *testing.T) {
	t.Parallel()

	var (
		addSubnetCalls    int32
		lastAddSubnetBody atomic.Value
	)

	srv := newNetworkReconcileServer(t, "10.100.0.0/16", `[]`, &addSubnetCalls, &lastAddSubnetBody)
	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

	network, err := prov.EnsureNetwork(context.Background(), "test-cluster", "10.100.0.0/16")

	require.NoError(t, err)
	require.NotNil(t, network)
	assert.Equal(t, int32(1), atomic.LoadInt32(&addSubnetCalls))
	assert.Contains(t, lastAddSubnetBody.Load(), `"ip_range":"10.100.0.0/24"`,
		"a custom /16 network must get its first /24 as the server subnet, not the full range")
}

// TestEnsureNetwork_CustomCIDRSkipsWhenFirstSlash24Present proves the idempotency
// check compares against the DERIVED /24 for custom ranges, so a network already
// carrying its first /24 is left as-is.
func TestEnsureNetwork_CustomCIDRSkipsWhenFirstSlash24Present(t *testing.T) {
	t.Parallel()

	var (
		addSubnetCalls    int32
		lastAddSubnetBody atomic.Value
	)

	existingSubnet := `[{"type":"cloud","ip_range":"10.100.0.0/24","network_zone":"eu-central","gateway":"10.100.0.1"}]`
	srv := newNetworkReconcileServer(
		t, "10.100.0.0/16", existingSubnet, &addSubnetCalls, &lastAddSubnetBody,
	)
	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

	network, err := prov.EnsureNetwork(context.Background(), "test-cluster", "10.100.0.0/16")

	require.NoError(t, err)
	require.NotNil(t, network)
	assert.Equal(t, int32(0), atomic.LoadInt32(&addSubnetCalls),
		"the idempotency check must match the derived first /24, not the full range")
}

// TestEnsureNetwork_CustomSlash24UsesWholeRange proves a custom range that is
// already /24 (no room for a smaller carve-out) is used whole as the server
// subnet.
func TestEnsureNetwork_CustomSlash24UsesWholeRange(t *testing.T) {
	t.Parallel()

	var (
		addSubnetCalls    int32
		lastAddSubnetBody atomic.Value
	)

	srv := newNetworkReconcileServer(t, "10.200.5.0/24", `[]`, &addSubnetCalls, &lastAddSubnetBody)
	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

	network, err := prov.EnsureNetwork(context.Background(), "test-cluster", "10.200.5.0/24")

	require.NoError(t, err)
	require.NotNil(t, network)
	assert.Equal(t, int32(1), atomic.LoadInt32(&addSubnetCalls))
	assert.Contains(t, lastAddSubnetBody.Load(), `"ip_range":"10.200.5.0/24"`,
		"a /24 network uses its whole range as the server subnet")
}
