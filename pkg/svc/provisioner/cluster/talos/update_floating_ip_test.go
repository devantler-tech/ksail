package talosprovisioner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

// fipUpdateSecondControlPlaneServerJSON is the post-scale control-plane server
// used to prove the final floating-IP refresh rebuilds SANs from live inventory.
const fipUpdateSecondControlPlaneServerJSON = `{"id":12,"name":"fip-cluster-cp-1","status":"running",` +
	`"public_net":{"ipv4":{"ip":"203.0.113.6","blocked":false,"dns_ptr":""},"ipv6":null,` +
	`"floating_ips":[]},"private_net":[],` +
	`"server_type":{"id":1,"name":"cx22","description":"","cores":2,"memory":4,"disk":40},` +
	`"labels":{"ksail.owned":"true","ksail.cluster.name":"fip-cluster",` +
	`"ksail.node.type":"controlplane","ksail.node.index":"1"},` +
	`"created":"2026-07-02T00:00:00+00:00"}`

// fipUpdateWorkerServerJSON is the running worker used to detect a partial
// floating-IP apply that updated control planes but left workers stale.
const fipUpdateWorkerServerJSON = `{"id":21,"name":"fip-cluster-worker-0","status":"running",` +
	`"public_net":{"ipv4":{"ip":"203.0.113.20","blocked":false,"dns_ptr":""},"ipv6":null,` +
	`"floating_ips":[]},"private_net":[],` +
	`"server_type":{"id":1,"name":"cx22","description":"","cores":2,"memory":4,"disk":40},` +
	`"labels":{"ksail.owned":"true","ksail.cluster.name":"fip-cluster",` +
	`"ksail.node.type":"worker","ksail.node.index":"0"},` +
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

	return fipUpdateTestServerWithServers(
		t, floatingIPPresent, calls, fipUpdateControlPlaneServerJSON,
	)
}

// fipUpdateTestServerWithServers is fipUpdateTestServer with a caller-supplied
// server list, used by topology-change refresh tests.
func fipUpdateTestServerWithServers(
	t *testing.T,
	floatingIPPresent bool,
	calls *fipUpdateCalls,
	serversJSON ...string,
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
	mux.HandleFunc(
		"/servers",
		func(responseWriter http.ResponseWriter, request *http.Request) {
			responseWriter.Header().Set("Content-Type", "application/json")

			selected := serversJSON
			if name := request.URL.Query().Get("name"); name != "" {
				selected = nil

				for _, candidate := range serversJSON {
					if strings.Contains(candidate, `"name":"`+name+`"`) {
						selected = append(selected, candidate)
					}
				}
			}

			_, _ = responseWriter.Write(
				[]byte(`{"servers":[` + strings.Join(selected, ",") + `]}`),
			)
		},
	)

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

// TestDiffConfig_FloatingIPDriftSurfacesBeforeUpdate verifies the CLI preflight
// sees live floating-IP drift through DiffConfig. Without this signal, the
// orchestrator reports "No changes detected" and never invokes Update, so the
// reconcile path cannot repair an enabled-but-absent address (#5947).
func TestDiffConfig_FloatingIPDriftSurfacesBeforeUpdate(t *testing.T) {
	t.Parallel()

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, false, calls)

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

	current := &v1alpha1.ClusterSpec{ControlPlanes: 1}
	desired := current.DeepCopy()

	diff, err := provisioner.DiffConfig(t.Context(), "fip-cluster", current, desired)
	require.NoError(t, err)
	assert.Contains(t, diff.InPlaceChanges, clusterupdate.Change{
		Field:    floatingIPEnabledField,
		OldValue: "false",
		NewValue: "true",
		Category: clusterupdate.ChangeCategoryInPlace,
		Reason: "the floating IP is created and attached, and control planes " +
			"receive the VIP config without reboot",
	})
	assert.Equal(t, int32(0), calls.create.Load(),
		"preflight detection must remain read-only")
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
	server := fipUpdateTestServerWithServers(
		t, true, calls, fipUpdateControlPlaneServerJSON, fipUpdateWorkerServerJSON,
	)
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
	runningControlPlane := configuredProvisioner.TalosConfigsForTest().ControlPlane()
	runningWorker := configuredProvisioner.TalosConfigsForTest().Worker()

	// A fresh provisioner models the next CLI invocation: its generated config
	// has no runtime VIP patch, so idempotency must be based on the running node.
	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
		TokenEnvVar:        testFloatingIPTokenEnvVar,
	}).WithInfraProvider(hzProvider).
		WithNodeConfigFetcherForTest(
			func(_ context.Context, nodeIP string) (talosconfig.Provider, error) {
				if nodeIP == "203.0.113.20" {
					return runningWorker, nil
				}

				return runningControlPlane, nil
			},
		)

	diff := &clusterupdate.UpdateResult{}
	require.NoError(t,
		provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff))

	assert.Empty(t, diff.InPlaceChanges,
		"an existing owned floating IP with stored VIP config must not re-diff")
}

// TestMergeFloatingIPChanges_ConfiguredControlPlanesStaleWorkerAddsChange
// verifies a partial prior apply is retried when control planes carry the
// floating endpoint/VIP but a worker still points at a direct control-plane IP.
func TestMergeFloatingIPChanges_ConfiguredControlPlanesStaleWorkerAddsChange(t *testing.T) {
	t.Setenv(testFloatingIPTokenEnvVar, "vip-test-token")

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServerWithServers(
		t, true, calls, fipUpdateControlPlaneServerJSON, fipUpdateWorkerServerJSON,
	)
	hzProvider := newFipUpdateProvider(server.URL)
	options := v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
		TokenEnvVar:        testFloatingIPTokenEnvVar,
	}

	configuredProvisioner := newFloatingIPTestProvisioner(t, options).
		WithInfraProvider(hzProvider)
	require.NoError(t, configuredProvisioner.UpdateConfigsWithEndpointForTest(
		t.Context(), hzProvider, "fip-cluster",
		[]*hcloud.Server{controlPlaneServer(11, "fip-cluster-cp-0", "203.0.113.5")},
	))
	configuredControlPlane := configuredProvisioner.TalosConfigsForTest().ControlPlane()
	staleWorker := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{}).
		TalosConfigsForTest().Worker()

	provisioner := newFloatingIPTestProvisioner(t, options).
		WithInfraProvider(hzProvider).
		WithNodeConfigFetcherForTest(
			func(_ context.Context, nodeIP string) (talosconfig.Provider, error) {
				if nodeIP == "203.0.113.20" {
					return staleWorker, nil
				}

				return configuredControlPlane, nil
			},
		)

	diff := clusterupdate.NewEmptyUpdateResult()
	require.NoError(t,
		provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff))

	require.Len(t, diff.InPlaceChanges, 1)
	assert.Equal(t, floatingIPEnabledField, diff.InPlaceChanges[0].Field)
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

// TestMergeFloatingIPChanges_DisabledIgnoresUnownedCollision verifies external
// same-name addresses are irrelevant while floating-IP management is disabled.
func TestMergeFloatingIPChanges_DisabledIgnoresUnownedCollision(t *testing.T) {
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

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{}).
		WithInfraProvider(newFipUpdateProvider(server.URL))
	diff := &clusterupdate.UpdateResult{}

	err := provisioner.MergeFloatingIPChangesForTest(t.Context(), "fip-cluster", diff)

	require.NoError(t, err)
	assert.Empty(t, diff.InPlaceChanges)
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

// TestUpdate_FloatingIPDetectionFailure verifies an enabled floating-IP update
// fails closed when HCloud state cannot be read instead of succeeding without
// reconciling the endpoint.
func TestUpdate_FloatingIPDetectionFailure(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc(
		"/floating_ips",
		func(responseWriter http.ResponseWriter, _ *http.Request) {
			http.Error(responseWriter, "provider unavailable", http.StatusInternalServerError)
		},
	)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled: true,
	}).WithInfraProvider(newFipUpdateProvider(server.URL))
	spec := &v1alpha1.ClusterSpec{ControlPlanes: 1}

	_, err := provisioner.Update(
		t.Context(), "fip-cluster", spec, spec, clusterupdate.UpdateOptions{},
	)

	require.ErrorContains(t, err, "failed to detect floating IP state")
}

// TestMergeFloatingIPChanges_PropagatesContextTermination verifies cancellation
// and deadline errors abort live drift detection instead of degrading into a
// successful unavailable-state skip and a misleading no-change result.
func TestMergeFloatingIPChanges_PropagatesContextTermination(t *testing.T) {
	t.Parallel()

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, false, calls)

	tests := []struct {
		name       string
		newContext func() context.Context
		want       error
	}{
		{
			name: "canceled",
			newContext: func() context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()

				return ctx
			},
			want: context.Canceled,
		},
		{
			name: "deadline exceeded",
			newContext: func() context.Context {
				ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				cancel()

				return ctx
			},
			want: context.DeadlineExceeded,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
				FloatingIPEnabled: true,
			}).WithInfraProvider(newFipUpdateProvider(server.URL))
			diff := &clusterupdate.UpdateResult{}

			err := provisioner.MergeFloatingIPChangesForTest(
				test.newContext(), "fip-cluster", diff,
			)

			require.ErrorIs(t, err, test.want)
			assert.Empty(t, diff.InPlaceChanges)
		})
	}
}

// TestMergeFloatingIPChanges_ConfigDetectionPropagatesContextTermination pins
// cancellation handling in the second, running-config detection phase after an
// owned floating IP has already been found.
func TestMergeFloatingIPChanges_ConfigDetectionPropagatesContextTermination(t *testing.T) {
	t.Parallel()

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, true, calls)

	tests := []struct {
		name string
		err  error
	}{
		{name: "canceled", err: context.Canceled},
		{name: "deadline exceeded", err: context.DeadlineExceeded},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
				FloatingIPEnabled: true,
			}).WithInfraProvider(newFipUpdateProvider(server.URL)).
				WithNodeConfigFetcherForTest(
					func(context.Context, string) (talosconfig.Provider, error) {
						return nil, test.err
					},
				)
			diff := &clusterupdate.UpdateResult{}

			err := provisioner.MergeFloatingIPChangesForTest(
				t.Context(), "fip-cluster", diff,
			)

			require.ErrorIs(t, err, test.err)
			assert.Empty(t, diff.InPlaceChanges)
		})
	}
}

// TestRefreshFloatingIPEndpointAfterNodeChanges_RefreshesControlPlaneInventory
// verifies a control-plane scale refreshes endpoint SANs even when live drift
// detection was clean before the topology change and produced no floating-IP diff.
func TestRefreshFloatingIPEndpointAfterNodeChanges_RefreshesControlPlaneInventory(t *testing.T) {
	t.Setenv(testFloatingIPTokenEnvVar, "vip-test-token")

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServerWithServers(
		t,
		true,
		calls,
		fipUpdateControlPlaneServerJSON,
		fipUpdateSecondControlPlaneServerJSON,
	)
	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
		TokenEnvVar:        testFloatingIPTokenEnvVar,
	}).WithInfraProvider(newFipUpdateProvider(server.URL))

	err := provisioner.RefreshFloatingIPEndpointAfterNodeChangesForTest(
		t.Context(),
		"fip-cluster",
		&v1alpha1.ClusterSpec{ControlPlanes: 1},
		&v1alpha1.ClusterSpec{ControlPlanes: 2},
		clusterupdate.NewEmptyUpdateResult(),
	)
	require.NoError(t, err)

	config := provisioner.TalosConfigsForTest().ControlPlane()
	certSANs := config.Cluster().CertSANs()
	assert.Contains(t, certSANs, "192.0.2.10")
	assert.Contains(t, certSANs, "203.0.113.5")
	assert.Contains(t, certSANs, "203.0.113.6",
		"the post-scale control plane must be added to endpoint SANs")
	assert.Equal(t, int32(1), calls.assign.Load())
}

// TestUpdateApplyStep_PreparesFloatingIPBeforeControlPlaneRoll verifies a
// topology-only update refreshes the generated VIP bundle before replacement,
// even when live drift detection was clean and emitted no floating-IP change.
func TestUpdateApplyStep_PreparesFloatingIPBeforeControlPlaneRoll(t *testing.T) {
	t.Setenv(testFloatingIPTokenEnvVar, "vip-test-token")

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, true, calls)
	hzProvider := newFipUpdateProvider(server.URL)
	options := v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
		TokenEnvVar:        testFloatingIPTokenEnvVar,
	}
	configuredProvisioner := newFloatingIPTestProvisioner(t, options).
		WithInfraProvider(hzProvider)
	require.NoError(t, configuredProvisioner.UpdateConfigsWithEndpointForTest(
		t.Context(), hzProvider, "fip-cluster",
		[]*hcloud.Server{controlPlaneServer(11, "fip-cluster-cp-0", "203.0.113.5")},
	))
	runningControlPlane := configuredProvisioner.TalosConfigsForTest().ControlPlane()
	kubeconfigPath := t.TempDir() + "/kubeconfig"
	provisioner := newFloatingIPTestProvisionerWithOptions(
		t, options, talosprovisioner.NewOptions().WithKubeconfigPath(kubeconfigPath),
	).
		WithInfraProvider(hzProvider).
		WithNodeConfigFetcherForTest(
			func(context.Context, string) (talosconfig.Provider, error) {
				return runningControlPlane, nil
			},
		)
	capture := &floatingIPKubeconfigCapture{}

	provisioner.WithTalosClientFactoryForTest(
		func(_ context.Context, endpoint string) (talosprovisioner.KubeconfigFetcherForTest, error) {
			capture.calls++
			capture.talosEndpoint = endpoint

			return &mockKubeconfigFetcher{kubeconfig: minimalKubeconfigBytes()}, nil
		},
	)

	diff := clusterupdate.NewEmptyUpdateResult()
	result := clusterupdate.NewEmptyUpdateResult()
	result.RollingRecreate = append(result.RollingRecreate, clusterupdate.Change{
		Field:    "provider.hetzner.controlPlaneServerType",
		Category: clusterupdate.ChangeCategoryRollingRecreate,
	})
	spec := &v1alpha1.ClusterSpec{ControlPlanes: 3}

	require.False(t, provisioner.HasDesiredHetznerFloatingIPEndpointForTest())
	require.NoError(t, provisioner.RunUpdateApplyStepForTest(
		t.Context(), "reconcile floating IP endpoint", "fip-cluster",
		spec, spec, diff, result,
	))
	assert.True(t, provisioner.HasDesiredHetznerFloatingIPEndpointForTest(),
		"replacement control planes must receive the VIP config before the roll starts")
	assert.Equal(t, 1, capture.calls)
	assert.Equal(t, "203.0.113.5", capture.talosEndpoint)

	written, err := os.ReadFile(kubeconfigPath) //nolint:gosec // test-owned path
	require.NoError(t, err)
	assert.Contains(t, string(written), "https://192.0.2.10:6443")
}

// TestUpdateApplyStep_DoesNotPrepareFloatingIPForWorkerRoll keeps the pre-roll
// refresh bounded to control-plane topology: a worker-only roll must not touch
// the Hetzner floating-IP API.
func TestUpdateApplyStep_DoesNotPrepareFloatingIPForWorkerRoll(t *testing.T) {
	t.Parallel()

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled: true,
	})
	diff := clusterupdate.NewEmptyUpdateResult()
	result := clusterupdate.NewEmptyUpdateResult()
	result.RollingRecreate = append(result.RollingRecreate, clusterupdate.Change{
		Field:    "provider.hetzner.workerServerType",
		Category: clusterupdate.ChangeCategoryRollingRecreate,
	})
	spec := &v1alpha1.ClusterSpec{ControlPlanes: 3, Workers: 1}

	require.NoError(t, provisioner.RunUpdateApplyStepForTest(
		t.Context(), "reconcile floating IP endpoint", "fip-cluster",
		spec, spec, diff, result,
	))
	assert.False(t, provisioner.HasDesiredHetznerFloatingIPEndpointForTest())
}

type floatingIPKubeconfigCapture struct {
	calls         int
	talosEndpoint string
}

// newFloatingIPKubeconfigTestProvisioner builds an already-reconciled
// floating-IP provisioner with a captured Talos kubeconfig fetcher.
func newFloatingIPKubeconfigTestProvisioner(
	t *testing.T,
	serverURL string,
) (*talosprovisioner.Provisioner, string, *floatingIPKubeconfigCapture) {
	t.Helper()
	t.Setenv(testFloatingIPTokenEnvVar, "vip-test-token")

	kubeconfigPath := t.TempDir() + "/kubeconfig"
	hzProvider := newFipUpdateProvider(serverURL)
	options := talosprovisioner.NewOptions().WithKubeconfigPath(kubeconfigPath)
	provisioner := newFloatingIPTestProvisionerWithOptions(
		t,
		v1alpha1.OptionsHetzner{
			FloatingIPEnabled:  true,
			FloatingIPLocation: "fsn1",
			TokenEnvVar:        testFloatingIPTokenEnvVar,
		},
		options,
	).WithInfraProvider(hzProvider)
	require.NoError(t, provisioner.UpdateConfigsWithEndpointForTest(
		t.Context(), hzProvider, "fip-cluster",
		[]*hcloud.Server{controlPlaneServer(11, "fip-cluster-cp-0", "203.0.113.5")},
	))

	capture := &floatingIPKubeconfigCapture{}

	provisioner.WithTalosClientFactoryForTest(
		func(_ context.Context, endpoint string) (talosprovisioner.KubeconfigFetcherForTest, error) {
			capture.calls++
			capture.talosEndpoint = endpoint

			return &mockKubeconfigFetcher{kubeconfig: minimalKubeconfigBytes()}, nil
		},
	)

	return provisioner, kubeconfigPath, capture
}

func floatingIPChangeResult() *clusterupdate.UpdateResult {
	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field:    floatingIPEnabledField,
		Category: clusterupdate.ChangeCategoryInPlace,
	})

	return diff
}

// TestUpdateApplyStep_RefreshesFloatingIPKubeconfig verifies the update fetches
// kubeconfig through a reachable direct control-plane Talos endpoint while the
// persisted Kubernetes server uses the stable floating IP.
//
//nolint:paralleltest // helper uses t.Setenv.
func TestUpdateApplyStep_RefreshesFloatingIPKubeconfig(t *testing.T) {
	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, true, calls)
	provisioner, kubeconfigPath, capture := newFloatingIPKubeconfigTestProvisioner(t, server.URL)
	spec := &v1alpha1.ClusterSpec{ControlPlanes: 1}

	require.NoError(t, provisioner.RunUpdateApplyStepForTest(
		t.Context(), "refresh floating IP kubeconfig", "fip-cluster",
		spec, spec, floatingIPChangeResult(), clusterupdate.NewEmptyUpdateResult(),
	))
	assert.Equal(t, "203.0.113.5", capture.talosEndpoint,
		"Talos API fetches must keep using a directly reachable node")

	written, err := os.ReadFile(kubeconfigPath) //nolint:gosec // test-owned path
	require.NoError(t, err)
	assert.Contains(t, string(written), "https://192.0.2.10:6443",
		"the persisted Kubernetes endpoint must use the floating IP")
}

// TestUpdateApplyStep_DoesNotRefreshKubeconfigAfterPartialApply verifies a
// failed node config push never switches client access to an endpoint that may
// not have reached every node.
//
//nolint:paralleltest // helper uses t.Setenv.
func TestUpdateApplyStep_DoesNotRefreshKubeconfigAfterPartialApply(t *testing.T) {
	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, true, calls)
	provisioner, kubeconfigPath, capture := newFloatingIPKubeconfigTestProvisioner(t, server.URL)
	result := clusterupdate.NewEmptyUpdateResult()
	result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{Field: "talos.config"})
	spec := &v1alpha1.ClusterSpec{ControlPlanes: 1}

	require.NoError(t, provisioner.RunUpdateApplyStepForTest(
		t.Context(), "refresh floating IP kubeconfig", "fip-cluster",
		spec, spec, floatingIPChangeResult(), result,
	))
	assert.Zero(t, capture.calls)

	_, err := os.Stat(kubeconfigPath)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// TestUpdateApplyStep_RefreshKubeconfigWithoutControlPlaneFails verifies an
// empty live inventory returns a typed error instead of indexing an empty list.
//
//nolint:paralleltest // helper uses t.Setenv.
func TestUpdateApplyStep_RefreshKubeconfigWithoutControlPlaneFails(t *testing.T) {
	calls := &fipUpdateCalls{}
	configuredServer := fipUpdateTestServer(t, true, calls)
	provisioner, _, _ := newFloatingIPKubeconfigTestProvisioner(t, configuredServer.URL)
	emptyServer := fipUpdateTestServerWithServers(t, true, calls)
	provisioner.WithInfraProvider(newFipUpdateProvider(emptyServer.URL))

	spec := &v1alpha1.ClusterSpec{ControlPlanes: 1}

	err := provisioner.RunUpdateApplyStepForTest(
		t.Context(), "refresh floating IP kubeconfig", "fip-cluster",
		spec, spec, floatingIPChangeResult(), clusterupdate.NewEmptyUpdateResult(),
	)
	require.ErrorIs(t, err, talosprovisioner.ErrNoControlPlaneForRefresh)
}

// TestUpdateApplyStep_ControlPlaneRollRequiresVerifiedFloatingIPState verifies
// an unavailable live config read fails closed before kubeconfig is switched.
//
//nolint:paralleltest // helper uses t.Setenv.
func TestUpdateApplyStep_ControlPlaneRollRequiresVerifiedFloatingIPState(t *testing.T) {
	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, true, calls)
	provisioner, kubeconfigPath, capture := newFloatingIPKubeconfigTestProvisioner(t, server.URL)
	provisioner.WithNodeConfigFetcherForTest(
		func(context.Context, string) (talosconfig.Provider, error) {
			return nil, assert.AnError
		},
	)

	result := clusterupdate.NewEmptyUpdateResult()
	result.RollingRecreate = append(result.RollingRecreate, clusterupdate.Change{
		Field:    "provider.hetzner.controlPlaneServerType",
		Category: clusterupdate.ChangeCategoryRollingRecreate,
	})
	spec := &v1alpha1.ClusterSpec{ControlPlanes: 3}

	err := provisioner.RunUpdateApplyStepForTest(
		t.Context(), "reconcile floating IP endpoint", "fip-cluster",
		spec, spec, clusterupdate.NewEmptyUpdateResult(), result,
	)
	require.ErrorIs(t, err, talosprovisioner.ErrFloatingIPReconcileBeforeControlPlaneRoll)
	assert.Zero(t, capture.calls)

	_, statErr := os.Stat(kubeconfigPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

// TestUpdateApplySteps_FloatingIPEndpointOrdering pins the safe sequence: plan
// validation first, pre-roll VIP preparation before replacement, and kubeconfig
// persistence only after the refreshed bundle has been pushed to running nodes.
func TestUpdateApplySteps_FloatingIPEndpointOrdering(t *testing.T) {
	t.Parallel()

	names := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{}).
		UpdateApplyStepNamesForTest()
	require.NotEmpty(t, names)
	assert.Equal(t, "validate update plan", names[0])

	reconcileIndex := slices.Index(names, "reconcile floating IP endpoint")
	rollIndex := slices.Index(names, "apply rolling recreate changes")
	applyIndex := slices.Index(names, "apply in-place config changes")
	refreshKubeconfigIndex := slices.Index(names, "refresh floating IP kubeconfig")
	rebootIndex := slices.Index(names, "apply reboot-required changes")

	require.NotEqual(t, -1, refreshKubeconfigIndex)
	assert.Less(t, reconcileIndex, rollIndex)
	assert.Less(t, rollIndex, applyIndex)
	assert.Less(t, applyIndex, refreshKubeconfigIndex)
	assert.Less(t, refreshKubeconfigIndex, rebootIndex)
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

// TestBuildDesiredNodeConfig_PreservesReconciledFloatingIPEndpoint verifies the
// in-place config push keeps the authoritative endpoint generated by the
// floating-IP reconcile instead of realigning it back to each node's stale
// direct endpoint.
func TestBuildDesiredNodeConfig_PreservesReconciledFloatingIPEndpoint(t *testing.T) {
	t.Setenv(testFloatingIPTokenEnvVar, "vip-test-token")

	calls := &fipUpdateCalls{}
	server := fipUpdateTestServer(t, true, calls)
	hzProvider := newFipUpdateProvider(server.URL)
	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
		TokenEnvVar:        testFloatingIPTokenEnvVar,
	}).WithInfraProvider(hzProvider)

	runningControlPlane := provisioner.TalosConfigsForTest().ControlPlane()
	runningWorker := provisioner.TalosConfigsForTest().Worker()

	require.NotEqual(t, "192.0.2.10", runningControlPlane.Cluster().Endpoint().Hostname())

	require.NoError(t, provisioner.UpdateConfigsWithEndpointForTest(
		t.Context(), hzProvider, "fip-cluster",
		[]*hcloud.Server{controlPlaneServer(11, "fip-cluster-cp-0", "203.0.113.5")},
	))
	require.True(t, provisioner.HasDesiredHetznerFloatingIPEndpointForTest(),
		"the fixture must exercise the authoritative desired-endpoint branch")

	tests := []struct {
		name    string
		role    string
		running talosconfig.Provider
	}{
		{
			name:    "control plane",
			role:    talosprovisioner.RoleControlPlane,
			running: runningControlPlane,
		},
		{name: "worker", role: talosprovisioner.RoleWorker, running: runningWorker},
	}

	for _, test := range tests { //nolint:paralleltest // parent t.Setenv prevents parallel subtests.
		t.Run(test.name, func(t *testing.T) {
			desired, err := provisioner.BuildDesiredNodeConfigForTest(
				test.running, runningControlPlane, test.role,
			)
			require.NoError(t, err)
			assert.Equal(t, "192.0.2.10", desired.Cluster().Endpoint().Hostname(),
				"the in-place push must preserve the reconciled floating-IP endpoint")
		})
	}
}
