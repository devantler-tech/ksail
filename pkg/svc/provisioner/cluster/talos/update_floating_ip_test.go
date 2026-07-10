package talosprovisioner_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	hetzner "github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// floatingIPEnabledField mirrors the diff field name mergeFloatingIPChanges
// emits, asserted literally so a rename shows up in this test.
const floatingIPEnabledField = "provider.hetzner.floatingIPEnabled"

// fipUpdateCalls counts the hcloud API calls the update-reconcile tests care
// about.
type fipUpdateCalls struct {
	create atomic.Int32
	assign atomic.Int32
}

// fipUpdateOwnedFloatingIPJSON is the canned owned floating IP the update
// reconcile tests serve and create.
const fipUpdateOwnedFloatingIPJSON = `{"id":7,"name":"fip-cluster-floating-ip","description":"",` +
	`"ip":"192.0.2.10","type":"ipv4","server":null,"dns_ptr":[],` +
	`"home_location":{"id":1,"name":"fsn1","description":"","country":"DE","city":"",` +
	`"latitude":0,"longitude":0,"network_zone":"eu-central"},` +
	`"blocked":false,"protection":{"delete":false},` +
	`"labels":{"ksail.owned":"true","ksail.cluster.name":"fip-cluster"},` +
	`"created":"2026-07-02T00:00:00+00:00"}`

// fipUpdateControlPlaneServerJSON is the canned running control-plane server
// backing ListNodes/GetServerByName in the update reconcile tests.
const fipUpdateControlPlaneServerJSON = `{"id":11,"name":"fip-cluster-cp-0","status":"running",` +
	`"public_net":{"ipv4":{"ip":"203.0.113.5","blocked":false,"dns_ptr":""},"ipv6":null,` +
	`"floating_ips":[]},"private_net":[],` +
	`"server_type":{"id":1,"name":"cx22","description":"","cores":2,"memory":4,"disk":40},` +
	`"labels":{"ksail.owned":"true","ksail.cluster.name":"fip-cluster",` +
	`"ksail.node.type":"controlplane","ksail.node.index":"0"},` +
	`"created":"2026-07-02T00:00:00+00:00"}`

// fipUpdateTestServer answers the hcloud API calls the floating-IP update
// reconcile path makes: floating-IP get-by-name (present or absent), create,
// assign, and the /servers list backing ListNodes/GetServerByName with one
// running control-plane server.
func fipUpdateTestServer(
	t *testing.T,
	floatingIPPresent bool,
	calls *fipUpdateCalls,
) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc(
		"/floating_ips",
		func(responseWriter http.ResponseWriter, request *http.Request) {
			responseWriter.Header().Set("Content-Type", "application/json")

			if request.Method == http.MethodPost {
				calls.create.Add(1)

				_, _ = responseWriter.Write([]byte(
					`{"floating_ip":` + fipUpdateOwnedFloatingIPJSON + `,"action":null}`))

				return
			}

			list := ""
			if floatingIPPresent {
				list = fipUpdateOwnedFloatingIPJSON
			}

			_, _ = responseWriter.Write([]byte(`{"floating_ips":[` + list + `]}`))
		},
	)

	mux.HandleFunc("/floating_ips/7/actions/assign", fipUpdateAssignHandler(calls))
	mux.HandleFunc("/servers", fipUpdateServersHandler)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server
}

// fipUpdateAssignHandler answers the floating-IP assign action, counting the
// attachments.
func fipUpdateAssignHandler(calls *fipUpdateCalls) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, _ *http.Request) {
		calls.assign.Add(1)
		fipUpdateAssignActionResponse(responseWriter)
	}
}

// fipUpdateAssignActionResponse writes the canned successful assign-action
// response, shared with floatingIPEndpointTestServer.
func fipUpdateAssignActionResponse(responseWriter http.ResponseWriter) {
	type assignAction struct {
		ID       int    `json:"id"`
		Command  string `json:"command"`
		Status   string `json:"status"`
		Progress int    `json:"progress"`
	}

	responseWriter.Header().Set("Content-Type", "application/json")

	body := struct {
		Action assignAction `json:"action"`
	}{
		Action: assignAction{
			ID:       1,
			Command:  "assign_floating_ip",
			Status:   "success",
			Progress: 100,
		},
	}

	err := json.NewEncoder(responseWriter).Encode(body)
	if err != nil {
		http.Error(responseWriter, err.Error(), http.StatusInternalServerError)
	}
}

// fipUpdateServersHandler answers the /servers list backing
// ListNodes/GetServerByName with the canned control-plane server.
func fipUpdateServersHandler(responseWriter http.ResponseWriter, _ *http.Request) {
	responseWriter.Header().Set("Content-Type", "application/json")
	_, _ = responseWriter.Write(
		[]byte(`{"servers":[` + fipUpdateControlPlaneServerJSON + `]}`),
	)
}

// newFipUpdateProvider builds a hetzner.Provider backed by the given httptest
// server.
func newFipUpdateProvider(serverURL string) *hetzner.Provider {
	return hetzner.NewProvider(hcloud.NewClient(
		hcloud.WithToken("test-token"),
		hcloud.WithEndpoint(serverURL),
	))
}

func TestMergeFloatingIPChanges_EnabledAndAbsentAddsChange(t *testing.T) {
	t.Parallel()

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, false, calls)

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
	}).WithInfraProvider(newFipUpdateProvider(server.URL))

	diff := &clusterupdate.UpdateResult{}
	provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff, nil)

	require.Len(t, diff.InPlaceChanges, 1)
	change := diff.InPlaceChanges[0]
	assert.Equal(t, floatingIPEnabledField, change.Field)
	assert.Equal(t, "false", change.OldValue)
	assert.Equal(t, "true", change.NewValue)
	assert.Equal(t, clusterupdate.ChangeCategoryInPlace, change.Category)
	assert.Equal(t, int32(0), calls.create.Load(),
		"detection must be read-only")
}

func TestMergeFloatingIPChanges_EnabledAndPresentIsNoop(t *testing.T) {
	t.Parallel()

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, true, calls)

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
	}).WithInfraProvider(newFipUpdateProvider(server.URL))

	diff := &clusterupdate.UpdateResult{}
	provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff, nil)

	assert.Empty(t, diff.InPlaceChanges,
		"an existing owned floating IP must not re-diff (idempotent re-run)")
}

func TestMergeFloatingIPChanges_DisabledWithPresentIPWarnsOnly(t *testing.T) {
	t.Parallel()

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, true, calls)

	var log bytes.Buffer

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPLocation: "fsn1",
	}).WithInfraProvider(newFipUpdateProvider(server.URL)).WithLogWriter(&log)

	diff := &clusterupdate.UpdateResult{}
	provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff, nil)

	assert.Empty(t, diff.InPlaceChanges,
		"the disable transition is deferred (#6032) and must not claim a reconcile")
	assert.Contains(t, log.String(), "does not reconcile the disable transition",
		"the deferred disable transition must warn instead of staying silent")
}

func TestMergeFloatingIPChanges_NoHetznerOptionsIsNoop(t *testing.T) {
	t.Parallel()

	// No Hetzner options and no infra provider: any API call would panic or
	// error, so an empty diff proves the guard short-circuits.
	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{})

	diff := &clusterupdate.UpdateResult{}
	provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff, nil)

	assert.Empty(t, diff.InPlaceChanges)
}

func TestReconcileFloatingIPEndpoint_NoopWithoutChange(t *testing.T) {
	t.Parallel()

	// A nil infra provider would error on any call-through; a nil return
	// proves the diff gate short-circuits first.
	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled: true,
	})

	err := provisioner.ReconcileFloatingIPEndpointForTest(
		t.Context(), "fip-cluster", &clusterupdate.UpdateResult{},
	)
	require.NoError(t, err)
}

// TestReconcileFloatingIPEndpoint_AppliesDetectedChange covers the #5947 apply
// path end-to-end against a fake hcloud API: the detected change makes the
// step create + attach the floating IP and regenerate the stored configs with
// the floating-IP endpoint, SANs, and control-plane VIP block — the state the
// in-place config push then delivers to the running control planes (no
// t.Parallel: t.Setenv provides the hcloud token the VIP block embeds).
func TestReconcileFloatingIPEndpoint_AppliesDetectedChange(t *testing.T) {
	t.Setenv(testFloatingIPTokenEnvVar, "vip-test-token")

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, false, calls)

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
		TokenEnvVar:        testFloatingIPTokenEnvVar,
	}).WithInfraProvider(newFipUpdateProvider(server.URL))

	diff := &clusterupdate.UpdateResult{}
	provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff, nil)
	require.Len(t, diff.InPlaceChanges, 1, "the absent floating IP must diff")

	err := provisioner.ReconcileFloatingIPEndpointForTest(t.Context(), "fip-cluster", diff)
	require.NoError(t, err)

	assert.Equal(t, int32(1), calls.create.Load(),
		"the absent floating IP must be created")
	assert.Equal(t, int32(1), calls.assign.Load(),
		"the floating IP must be attached to the control-plane server")

	configs := provisioner.TalosConfigsForTest()
	endpoint := configs.ControlPlane().Cluster().Endpoint()
	require.NotNil(t, endpoint)
	assert.Equal(t, "192.0.2.10", endpoint.Hostname(),
		"the regenerated endpoint must be the floating IP")
	assert.Contains(t, configs.ControlPlane().Cluster().CertSANs(), "203.0.113.5",
		"control-plane node IPs must stay in the SAN set")

	devices := configs.ControlPlane().Machine().Network().Devices()
	require.Len(t, devices, 1)

	vip := devices[0].VIPConfig()
	require.NotNil(t, vip)
	assert.Equal(t, "192.0.2.10", vip.IP(),
		"control planes must carry the VIP block for the floating IP")
}
