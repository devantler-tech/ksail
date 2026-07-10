package talosprovisioner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	hetzner "github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
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

// TestMergeFloatingIPChanges_EnabledAndAbsentAddsChange verifies that drifted
// enablement produces one in-place reconcile change.
func TestMergeFloatingIPChanges_EnabledAndAbsentAddsChange(t *testing.T) {
	t.Parallel()

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, false, calls)

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
	}).WithInfraProvider(newFipUpdateProvider(server.URL))

	diff := &clusterupdate.UpdateResult{}
	require.NoError(t,
		provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff))

	require.Len(t, diff.InPlaceChanges, 1)
	change := diff.InPlaceChanges[0]
	assert.Equal(t, floatingIPEnabledField, change.Field)
	assert.Equal(t, "false", change.OldValue)
	assert.Equal(t, "true", change.NewValue)
	assert.Equal(t, clusterupdate.ChangeCategoryInPlace, change.Category)
	assert.Equal(t, int32(0), calls.create.Load(),
		"detection must be read-only")
}

// TestMergeFloatingIPChanges_EnabledAndPresentMissingConfigAddsChange verifies
// recovery after a partial apply created the address but failed before storing
// the endpoint and VIP configuration.
func TestMergeFloatingIPChanges_EnabledAndPresentMissingConfigAddsChange(t *testing.T) {
	t.Parallel()

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, true, calls)

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
	}).WithInfraProvider(newFipUpdateProvider(server.URL))
	runningConfig := provisioner.TalosConfigsForTest().ControlPlane()
	provisioner.WithNodeConfigFetcherForTest(
		func(context.Context, string) (talosconfig.Provider, error) {
			return runningConfig, nil
		},
	)

	diff := &clusterupdate.UpdateResult{}
	require.NoError(t,
		provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff))

	require.Len(t, diff.InPlaceChanges, 1,
		"an existing address without stored VIP config must be reconciled again")
	assert.Equal(t, floatingIPEnabledField, diff.InPlaceChanges[0].Field)
}

// TestMergeFloatingIPChanges_EnabledAndConfiguredIsNoop verifies idempotency
// once both the owned address and stored endpoint/VIP configuration exist.
func TestMergeFloatingIPChanges_EnabledAndConfiguredIsNoop(t *testing.T) {
	t.Setenv(testFloatingIPTokenEnvVar, "vip-test-token")

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, true, calls)
	hzProvider := newFipUpdateProvider(server.URL)

	configuredProvisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
		TokenEnvVar:        testFloatingIPTokenEnvVar,
	}).WithInfraProvider(hzProvider)

	require.NoError(t, configuredProvisioner.UpdateConfigsWithEndpointForTest(
		t.Context(), hzProvider, "fip-cluster",
		[]*hcloud.Server{controlPlaneServer(11, "fip-cluster-cp-0", "203.0.113.5")},
	))
	runningConfig := configuredProvisioner.TalosConfigsForTest().ControlPlane()

	// A fresh provisioner models the next CLI invocation: its generated config
	// has no runtime VIP patch, so idempotency must be based on the running node.
	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
		TokenEnvVar:        testFloatingIPTokenEnvVar,
	}).WithInfraProvider(hzProvider).
		WithNodeConfigFetcherForTest(
			func(context.Context, string) (talosconfig.Provider, error) {
				return runningConfig, nil
			},
		)

	diff := &clusterupdate.UpdateResult{}
	require.NoError(t,
		provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff))

	assert.Empty(t, diff.InPlaceChanges,
		"an existing owned floating IP with stored VIP config must not re-diff")
}

// TestAllControlPlanesHaveHetznerFloatingIPConfig_RequiresEveryNode verifies
// that a partial in-place apply is still detected when only one control plane
// received the endpoint/VIP configuration.
func TestAllControlPlanesHaveHetznerFloatingIPConfig_RequiresEveryNode(t *testing.T) {
	t.Setenv(testFloatingIPTokenEnvVar, "vip-test-token")

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, true, calls)
	hzProvider := newFipUpdateProvider(server.URL)

	configuredProvisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
		TokenEnvVar:        testFloatingIPTokenEnvVar,
	})
	require.NoError(t, configuredProvisioner.UpdateConfigsWithEndpointForTest(
		t.Context(), hzProvider, "fip-cluster",
		[]*hcloud.Server{controlPlaneServer(11, "fip-cluster-cp-0", "203.0.113.5")},
	))

	configured := configuredProvisioner.TalosConfigsForTest().ControlPlane()
	missing := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{}).
		TalosConfigsForTest().ControlPlane()

	assert.True(t, talosprovisioner.AllControlPlanesHaveHetznerFloatingIPConfigForTest(
		[]talosconfig.Provider{configured, configured}, "192.0.2.10",
	))
	assert.False(t, talosprovisioner.AllControlPlanesHaveHetznerFloatingIPConfigForTest(
		[]talosconfig.Provider{configured, missing}, "192.0.2.10",
	))
}

// TestMergeFloatingIPChanges_DisabledWithPresentIPWarnsOnly verifies that the
// deferred disable transition is visible without claiming a reconcile.
func TestMergeFloatingIPChanges_DisabledWithPresentIPWarnsOnly(t *testing.T) {
	t.Parallel()

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, true, calls)

	var log bytes.Buffer

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPLocation: "fsn1",
	}).WithInfraProvider(newFipUpdateProvider(server.URL)).WithLogWriter(&log)

	diff := &clusterupdate.UpdateResult{}
	require.NoError(t,
		provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff))

	assert.Empty(t, diff.InPlaceChanges,
		"the disable transition is deferred (#6032) and must not claim a reconcile")
	assert.Contains(t, log.String(), "does not reconcile the disable transition",
		"the deferred disable transition must warn instead of staying silent")
}

// TestMergeFloatingIPChanges_NoHetznerOptionsIsNoop verifies that non-Hetzner
// configurations skip cloud detection.
func TestMergeFloatingIPChanges_NoHetznerOptionsIsNoop(t *testing.T) {
	t.Parallel()

	// No Hetzner options and no infra provider: any API call would panic or
	// error, so an empty diff proves the guard short-circuits.
	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{})

	diff := &clusterupdate.UpdateResult{}
	require.NoError(t,
		provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff))

	assert.Empty(t, diff.InPlaceChanges)
}

// fipUpdateUnownedFloatingIPJSON is a floating IP carrying the cluster's
// conventional name but no ksail ownership labels — the collision case.
const fipUpdateUnownedFloatingIPJSON = `{"id":8,"name":"fip-cluster-floating-ip",` +
	`"description":"","ip":"192.0.2.20","type":"ipv4","server":null,"dns_ptr":[],` +
	`"home_location":{"id":1,"name":"fsn1","description":"","country":"DE","city":"",` +
	`"latitude":0,"longitude":0,"network_zone":"eu-central"},` +
	`"blocked":false,"protection":{"delete":false},"labels":{},` +
	`"created":"2026-07-02T00:00:00+00:00"}`

// TestMergeFloatingIPChanges_UnownedCollisionFailsUpdate pins the ownership
// guard at the update level: a same-name floating IP ksail does not own must
// fail the merge (and with it `cluster update`) instead of degrading into a
// successful no-op that skips the endpoint reconcile.
func TestMergeFloatingIPChanges_UnownedCollisionFailsUpdate(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc(
		"/floating_ips",
		func(responseWriter http.ResponseWriter, _ *http.Request) {
			responseWriter.Header().Set("Content-Type", "application/json")
			_, _ = responseWriter.Write(
				[]byte(`{"floating_ips":[` + fipUpdateUnownedFloatingIPJSON + `]}`))
		},
	)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	var log bytes.Buffer

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
	}).WithInfraProvider(newFipUpdateProvider(server.URL)).WithLogWriter(&log)

	diff := &clusterupdate.UpdateResult{}
	err := provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff)

	require.ErrorIs(t, err, hetzner.ErrFloatingIPNotOwned,
		"an ownership collision must propagate, not no-op the update")
	assert.Empty(t, diff.InPlaceChanges,
		"a collision must not claim a reconcilable change")
	assert.NotContains(t, log.String(), "Failed to detect floating IP state",
		"a collision is a definitive answer, not a warn-and-skip detection failure")
}

// TestReconcileFloatingIPEndpoint_NoopWithoutChange verifies that the apply
// step does not contact the provider without a matching diff.
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
	require.NoError(t,
		provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff))
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
