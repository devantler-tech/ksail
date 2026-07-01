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

// newNetworkReconcileServer returns an httptest server that answers the network
// GetByName lookup with a single existing network carrying the given subnets, and
// records how many times AddSubnet is invoked. It lets the reconcile tests assert
// whether EnsureNetwork adds the server subnet when adopting an existing network.
func newNetworkReconcileServer(
	t *testing.T,
	subnetsJSON string,
	addSubnetCalls *int32,
) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// GetByName lists networks filtered by name -> return one existing network.
	mux.HandleFunc("/networks", func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.Header().Set("Content-Type", "application/json")
		_, _ = responseWriter.Write([]byte(`{"networks":[{"id":42,"name":"test-cluster-network",` +
			`"ip_range":"10.0.0.0/16","subnets":` + subnetsJSON + `,"servers":[],"labels":{}}]}`))
	})

	// AddSubnet posts an action against the network id.
	mux.HandleFunc("/networks/42/actions/add_subnet",
		func(responseWriter http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(addSubnetCalls, 1)
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

	var addSubnetCalls int32

	srv := newNetworkReconcileServer(t, `[]`, &addSubnetCalls)
	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

	network, err := prov.EnsureNetwork(context.Background(), "test-cluster", defaultNetworkCIDR)

	require.NoError(t, err)
	require.NotNil(t, network)
	assert.Equal(t, int32(1), atomic.LoadInt32(&addSubnetCalls),
		"EnsureNetwork should add the missing subnet when adopting an existing subnet-less network")
}

// TestEnsureNetwork_SkipsSubnetWhenPresent proves the reconcile is idempotent:
// when the existing network already carries the expected server subnet, no
// duplicate AddSubnet call is made.
func TestEnsureNetwork_SkipsSubnetWhenPresent(t *testing.T) {
	t.Parallel()

	var addSubnetCalls int32

	existingSubnet := `[{"type":"cloud","ip_range":"10.0.1.0/24","network_zone":"eu-central","gateway":"10.0.0.1"}]`
	srv := newNetworkReconcileServer(t, existingSubnet, &addSubnetCalls)
	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

	network, err := prov.EnsureNetwork(context.Background(), "test-cluster", defaultNetworkCIDR)

	require.NoError(t, err)
	require.NotNil(t, network)
	assert.Equal(t, int32(0), atomic.LoadInt32(&addSubnetCalls),
		"EnsureNetwork should not re-add a subnet the network already carries")
}
