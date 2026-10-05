package talosprovisioner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/hetznercloud/hcloud-go/v2/hcloud/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	autoscalerFakeCluster   = "test-cluster"
	autoscalerFakeNetworkID = int64(100)
	otherClusterNetworkID   = int64(999)
	configuredPool          = "pool-a"
	removedPool             = "removed-pool"
)

// autoscalerHcloudAPI fakes the Hetzner endpoints autoscaler discovery reads. It
// applies a label selector the way the Hetzner API does — a bare key matches every
// server carrying that label, key=value matches that value — so a pool-scoped
// listing really does miss a removed pool's servers. Any other request would act on
// a server, so it is counted as unexpected and refused.
type autoscalerHcloudAPI struct {
	servers    []schema.Server
	listStatus atomic.Int32
	unexpected atomic.Int32
}

// newAutoscalerHcloudAPI serves the given servers and returns a Hetzner provider
// backed by the fake.
func newAutoscalerHcloudAPI(
	t *testing.T,
	servers ...schema.Server,
) (*hetzner.Provider, *autoscalerHcloudAPI) {
	t.Helper()

	api := &autoscalerHcloudAPI{servers: servers}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /networks", func(writer http.ResponseWriter, request *http.Request) {
		resp := schema.NetworkListResponse{}
		if request.URL.Query().Get("name") == autoscalerFakeCluster+hetzner.NetworkSuffix {
			resp.Networks = []schema.Network{{
				ID:   autoscalerFakeNetworkID,
				Name: autoscalerFakeCluster + hetzner.NetworkSuffix,
			}}
		}

		writeHcloudJSON(t, writer, resp)
	})

	mux.HandleFunc("GET /servers", func(writer http.ResponseWriter, request *http.Request) {
		if status := api.listStatus.Load(); status != 0 {
			http.Error(writer, "provider inventory unavailable", int(status))

			return
		}

		selector := request.URL.Query().Get("label_selector")

		writeHcloudJSON(t, writer, schema.ServerListResponse{Servers: api.matching(selector)})
	})

	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		api.unexpected.Add(1)

		http.Error(writer, "unexpected "+request.Method+" "+request.URL.Path, http.StatusBadRequest)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return hetzner.NewProvider(hcloud.NewClient(
		hcloud.WithToken("test-token"),
		hcloud.WithEndpoint(srv.URL),
	)), api
}

// matching returns the servers a Hetzner label selector of the form "key" or
// "key=value" selects.
func (api *autoscalerHcloudAPI) matching(selector string) []schema.Server {
	key, value, hasValue := strings.Cut(selector, "=")

	var matched []schema.Server

	for _, server := range api.servers {
		label, ok := server.Labels[key]
		if ok && (!hasValue || label == value) {
			matched = append(matched, server)
		}
	}

	return matched
}

func writeHcloudJSON(t *testing.T, writer http.ResponseWriter, body any) {
	t.Helper()

	writer.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(writer).Encode(body)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

// autoscalerServerSchema builds a running server the cluster autoscaler created for
// pool, attached to the private network with the given ID.
func autoscalerServerSchema(id int64, name, pool string, networkID int64) schema.Server {
	return schema.Server{
		ID:     id,
		Name:   name,
		Status: "running",
		Labels: map[string]string{hetzner.LabelAutoscalerNodeGroup: pool},
		PrivateNet: []schema.ServerPrivateNet{
			{Network: networkID, IP: "10.0.1.2"},
		},
	}
}

// autoscalerProvisioner returns a provisioner with the node autoscaler enabled and
// the given pools configured, backed by hzProvider.
func autoscalerProvisioner(
	hzProvider *hetzner.Provider,
	logWriter io.Writer,
	pools ...string,
) *talosprovisioner.Provisioner {
	nodePools := make([]v1alpha1.NodePool, 0, len(pools))
	for _, pool := range pools {
		nodePools = append(nodePools, v1alpha1.NodePool{Name: pool})
	}

	return talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(logWriter).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled:   true,
			AutoscalerNodePoolNames: pools,
			AutoscalerNodePools:     nodePools,
		}).
		WithInfraProvider(hzProvider)
}

// failedChangeReasons returns the reasons of the result's failed changes.
func failedChangeReasons(result *clusterupdate.UpdateResult) []string {
	reasons := make([]string, 0, len(result.FailedChanges))
	for _, change := range result.FailedChanges {
		reasons = append(reasons, change.Reason)
	}

	return reasons
}

type autoscalerDiscoveryCase struct {
	name         string
	pools        []string
	servers      []schema.Server
	wantServers  []string
	wantReported []string
}

// autoscalerDiscoveryCases enumerates the configured pools and the servers in the
// project, with the servers update must converge and the ones it must report.
func autoscalerDiscoveryCases() []autoscalerDiscoveryCase {
	return []autoscalerDiscoveryCase{
		{
			name:  "removed pool is reported, configured pool is converged",
			pools: []string{configuredPool},
			servers: []schema.Server{
				autoscalerServerSchema(1, "as-pool-a-1", configuredPool, autoscalerFakeNetworkID),
				autoscalerServerSchema(2, "as-removed-1", removedPool, autoscalerFakeNetworkID),
			},
			wantServers:  []string{"as-pool-a-1"},
			wantReported: []string{"as-removed-1"},
		},
		{
			name:  "last pool removed reports every server",
			pools: nil,
			servers: []schema.Server{
				autoscalerServerSchema(2, "as-removed-2", removedPool, autoscalerFakeNetworkID),
				autoscalerServerSchema(1, "as-removed-1", removedPool, autoscalerFakeNetworkID),
			},
			wantServers:  []string{},
			wantReported: []string{"as-removed-1", "as-removed-2"},
		},
		{
			name:  "another cluster's servers are ignored",
			pools: []string{configuredPool},
			servers: []schema.Server{
				autoscalerServerSchema(1, "other-pool-a", configuredPool, otherClusterNetworkID),
				autoscalerServerSchema(2, "other-removed", removedPool, otherClusterNetworkID),
			},
			wantServers:  []string{},
			wantReported: []string{},
		},
	}
}

// TestListAutoscalerServers_DiscoversEveryPoolOfTheCluster pins #7327: update
// discovery is cluster-wide, so a server of a pool removed from the configuration —
// including the last one — is reported as a failed change instead of being skipped,
// while a server in another cluster's network is neither returned nor reported.
func TestListAutoscalerServers_DiscoversEveryPoolOfTheCluster(t *testing.T) {
	t.Parallel()

	for _, testCase := range autoscalerDiscoveryCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			hzProvider, api := newAutoscalerHcloudAPI(t, testCase.servers...)
			prov := autoscalerProvisioner(hzProvider, io.Discard, testCase.pools...)
			result := clusterupdate.NewEmptyUpdateResult()

			servers, err := prov.ListAutoscalerServersForTest(
				context.Background(), autoscalerFakeCluster, result,
			)
			require.NoError(t, err)

			names := make([]string, 0, len(servers))
			for _, server := range servers {
				names = append(names, server.Name)
			}

			assert.Equal(t, testCase.wantServers, names)

			reasons := failedChangeReasons(result)
			require.Len(t, reasons, len(testCase.wantReported))

			for index, serverName := range testCase.wantReported {
				assert.Contains(t, reasons[index], serverName)
				assert.Contains(t, reasons[index], `"`+removedPool+`"`)
			}

			assert.Zero(t, api.unexpected.Load())
		})
	}
}

// TestPropagateAutoscalerBaseline_RefusesUnconfiguredPoolInEveryMode pins that no
// convergence mode — recycle, in-place reboot, NO_REBOOT apply — drains, reboots,
// reconfigures or deletes a server whose pool is no longer configured: each reports it
// as a failed change and makes no request that acts on a server. A server of another
// cluster that uses the configured pool name is neither touched nor reported.
func TestPropagateAutoscalerBaseline_RefusesUnconfiguredPoolInEveryMode(t *testing.T) {
	t.Parallel()

	modes := []struct {
		name         string
		diff         *clusterupdate.UpdateResult
		imageChanged bool
		noopMsg      string
	}{
		{"recycle", inPlaceDiff(), true, recycleNoopMsg},
		{"in-place reboot", rebootRequiredDiff(), false, rebootNoopMsg},
		{"in-place apply", inPlaceDiff(), false, inPlaceNoopMsg},
	}

	poolSets := []struct {
		name  string
		pools []string
	}{
		{"removed pool", []string{configuredPool}},
		{"last pool removed", nil},
	}

	for _, mode := range modes {
		for _, poolSet := range poolSets {
			t.Run(mode.name+"/"+poolSet.name, func(t *testing.T) {
				t.Parallel()

				hzProvider, api := newAutoscalerHcloudAPI(t, removedPoolAndOtherClusterServers()...)

				var logs bytes.Buffer

				prov := autoscalerProvisioner(hzProvider, &logs, poolSet.pools...)
				result := clusterupdate.NewEmptyUpdateResult()

				err := prov.PropagateAutoscalerBaselineForTest(
					context.Background(),
					autoscalerFakeCluster,
					mode.diff,
					mode.imageChanged,
					result,
				)
				require.NoError(t, err)

				reasons := failedChangeReasons(result)
				require.Len(t, reasons, 1)
				assert.Contains(t, reasons[0], "as-removed-1")
				assert.NotContains(t, reasons[0], "other-pool-a")
				assert.Zero(t, api.unexpected.Load(), "no request may act on a server")
				assert.Contains(t, logs.String(), "Autoscaler node as-removed-1 is left untouched")
				assert.Contains(t, logs.String(), mode.noopMsg)
			})
		}
	}
}

// removedPoolAndOtherClusterServers returns a server of this cluster whose pool is no
// longer configured, and a server of another cluster's network that uses the
// configured pool name.
func removedPoolAndOtherClusterServers() []schema.Server {
	return []schema.Server{
		autoscalerServerSchema(1, "as-removed-1", removedPool, autoscalerFakeNetworkID),
		autoscalerServerSchema(2, "other-pool-a", configuredPool, otherClusterNetworkID),
	}
}

// rebootRequiredDiff returns a diff carrying only a reboot-required change, which
// routes autoscaler convergence to the in-place reboot path.
func rebootRequiredDiff() *clusterupdate.UpdateResult {
	diff := clusterupdate.NewEmptyUpdateResult()
	diff.RebootRequired = append(diff.RebootRequired, clusterupdate.Change{})

	return diff
}

// TestReconcileAutoscalerNodes_ReportsUnconfiguredPoolOnEveryUpdate pins that a
// server of a removed pool is reported on every update, not only on the one that
// rewrites the autoscaler Secret. The update that removes the pool changes the Secret
// and reports the server while propagating; a later update leaves the Secret
// unchanged, propagates nothing, and must still report the server — exactly once in
// both cases, and without a request that acts on any server.
func TestReconcileAutoscalerNodes_ReportsUnconfiguredPoolOnEveryUpdate(t *testing.T) {
	t.Parallel()

	updates := []struct {
		name          string
		secretChanged bool
	}{
		{"secret changed", true},
		{"secret unchanged", false},
	}

	poolSets := []struct {
		name  string
		pools []string
	}{
		{"removed pool", []string{configuredPool}},
		{"last pool removed", nil},
	}

	for _, update := range updates {
		for _, poolSet := range poolSets {
			t.Run(update.name+"/"+poolSet.name, func(t *testing.T) {
				t.Parallel()

				hzProvider, api := newAutoscalerHcloudAPI(t, removedPoolAndOtherClusterServers()...)

				var logs bytes.Buffer

				prov := autoscalerProvisioner(hzProvider, &logs, poolSet.pools...)
				result := clusterupdate.NewEmptyUpdateResult()

				err := prov.ReconcileAutoscalerNodesForTest(
					context.Background(),
					autoscalerFakeCluster,
					inPlaceDiff(),
					update.secretChanged,
					false,
					result,
				)
				require.NoError(t, err)

				reasons := failedChangeReasons(result)
				require.Len(t, reasons, 1, "the leftover server is reported exactly once")
				assert.Contains(t, reasons[0], "as-removed-1")
				assert.Contains(t, reasons[0], `"`+removedPool+`"`)
				assert.NotContains(t, reasons[0], "other-pool-a")
				assert.Zero(t, api.unexpected.Load(), "no request may act on a server")
				assert.Equal(t, 1, strings.Count(
					logs.String(), "Autoscaler node as-removed-1 is left untouched",
				))
			})
		}
	}
}

// TestReconcileAutoscalerNodes_UnchangedSecretLeavesConfiguredServersAlone pins that
// the audit an unchanged Secret runs only reports: a server of a configured pool is
// neither reported nor converged, so an update that changes nothing stays a no-op.
func TestReconcileAutoscalerNodes_UnchangedSecretLeavesConfiguredServersAlone(t *testing.T) {
	t.Parallel()

	hzProvider, api := newAutoscalerHcloudAPI(t,
		autoscalerServerSchema(1, "as-pool-a-1", configuredPool, autoscalerFakeNetworkID),
	)

	var logs bytes.Buffer

	prov := autoscalerProvisioner(hzProvider, &logs, configuredPool)
	result := clusterupdate.NewEmptyUpdateResult()

	err := prov.ReconcileAutoscalerNodesForTest(
		context.Background(), autoscalerFakeCluster, inPlaceDiff(), false, false, result,
	)
	require.NoError(t, err)

	assert.Empty(t, result.FailedChanges)
	assert.Zero(t, api.unexpected.Load(), "no request may act on a server")
	assert.Empty(t, logs.String())
}

// disabledAutoscalerProvisioner returns a provisioner with the node autoscaler
// disabled and the given pools still listed in the configuration, backed by
// hzProvider.
func disabledAutoscalerProvisioner(
	hzProvider *hetzner.Provider,
	logWriter io.Writer,
	pools ...string,
) *talosprovisioner.Provisioner {
	nodePools := make([]v1alpha1.NodePool, 0, len(pools))
	for _, pool := range pools {
		nodePools = append(nodePools, v1alpha1.NodePool{Name: pool})
	}

	return talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(logWriter).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled:   false,
			AutoscalerNodePoolNames: pools,
			AutoscalerNodePools:     nodePools,
		}).
		WithInfraProvider(hzProvider)
}

// TestEnsureAutoscalerSecretIfNeeded_ReportsServersWhileAutoscalerDisabled pins that
// disabling the node autoscaler does not hide the servers it created. Nothing manages
// them any more — whether or not their pool is still listed — so each is reported
// exactly once as a failed change that names the disabled autoscaler, and none is
// acted on. A server of another cluster is neither touched nor reported.
func TestEnsureAutoscalerSecretIfNeeded_ReportsServersWhileAutoscalerDisabled(t *testing.T) {
	t.Parallel()

	poolSets := []struct {
		name  string
		pools []string
	}{
		{"pool removed", nil},
		{"pool still listed", []string{removedPool}},
	}

	for _, poolSet := range poolSets {
		t.Run(poolSet.name, func(t *testing.T) {
			t.Parallel()

			hzProvider, api := newAutoscalerHcloudAPI(t, removedPoolAndOtherClusterServers()...)

			var logs bytes.Buffer

			prov := disabledAutoscalerProvisioner(hzProvider, &logs, poolSet.pools...)
			result := clusterupdate.NewEmptyUpdateResult()

			err := prov.EnsureAutoscalerSecretIfNeededWithResultForTest(
				context.Background(), autoscalerFakeCluster, result,
			)
			require.NoError(t, err)

			reasons := failedChangeReasons(result)
			require.Len(t, reasons, 1, "the leftover server is reported exactly once")
			assert.Contains(t, reasons[0], "as-removed-1")
			assert.Contains(t, reasons[0], "node autoscaler is disabled")
			assert.NotContains(t, reasons[0], "other-pool-a")
			assert.Zero(t, api.unexpected.Load(), "no request may act on a server")
			assert.Equal(t, 1, strings.Count(
				logs.String(), "Autoscaler node as-removed-1 is left untouched",
			))
		})
	}
}

// TestEnsureAutoscalerSecretIfNeeded_SilentWithoutAutoscalerServers pins that a
// cluster that never used the node autoscaler is unaffected by the disabled-autoscaler
// audit: on Hetzner an empty listing reports and logs nothing, and with another
// infrastructure provider the Hetzner API is not consulted at all.
func TestEnsureAutoscalerSecretIfNeeded_SilentWithoutAutoscalerServers(t *testing.T) {
	t.Parallel()

	t.Run("hetzner cluster without autoscaler servers", func(t *testing.T) {
		t.Parallel()

		hzProvider, api := newAutoscalerHcloudAPI(t,
			autoscalerServerSchema(2, "other-pool-a", configuredPool, otherClusterNetworkID),
		)

		var logs bytes.Buffer

		prov := disabledAutoscalerProvisioner(hzProvider, &logs)
		result := clusterupdate.NewEmptyUpdateResult()

		err := prov.EnsureAutoscalerSecretIfNeededWithResultForTest(
			context.Background(), autoscalerFakeCluster, result,
		)
		require.NoError(t, err)

		assert.Empty(t, result.FailedChanges)
		assert.Empty(t, logs.String())
		assert.Zero(t, api.unexpected.Load())
	})

	t.Run("provider is not hetzner", func(t *testing.T) {
		t.Parallel()

		var logs bytes.Buffer

		prov := talosprovisioner.NewProvisioner(nil, nil).
			WithLogWriter(&logs).
			WithHetznerOptions(v1alpha1.OptionsHetzner{})
		result := clusterupdate.NewEmptyUpdateResult()

		err := prov.EnsureAutoscalerSecretIfNeededWithResultForTest(
			context.Background(), autoscalerFakeCluster, result,
		)
		require.NoError(t, err)

		assert.Empty(t, result.FailedChanges)
		assert.Empty(t, logs.String())
	})
}

// TestAuditUpdate_ReportsOnlyUnconfiguredServers exercises the public capability
// the no-diff command uses, including the last removed pool and foreign networks.
func TestAuditUpdate_ReportsOnlyUnconfiguredServers(t *testing.T) {
	t.Parallel()

	testCases := append(autoscalerDiscoveryCases(), autoscalerDiscoveryCase{
		name:  "configured servers are left untouched",
		pools: []string{configuredPool},
		servers: []schema.Server{
			autoscalerServerSchema(1, "as-pool-a-1", configuredPool, autoscalerFakeNetworkID),
		},
	})

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			hzProvider, api := newAutoscalerHcloudAPI(t, testCase.servers...)
			prov := autoscalerProvisioner(hzProvider, io.Discard, testCase.pools...)
			result := clusterupdate.NewEmptyUpdateResult()

			err := prov.AuditUpdate(t.Context(), autoscalerFakeCluster, result)
			require.NoError(t, err)

			reasons := failedChangeReasons(result)
			require.Len(t, reasons, len(testCase.wantReported))

			for index, serverName := range testCase.wantReported {
				assert.Contains(t, reasons[index], serverName)
				assert.Contains(t, reasons[index], "\""+removedPool+"\"")
			}

			assert.Zero(t, api.unexpected.Load(), "inventory audit must never mutate a server")
		})
	}
}

// Disabling the autoscaler must not hide its servers, whether their pool remains
// in the config or has been removed.
func TestAuditUpdate_ReportsDisabledAutoscalerServers(t *testing.T) {
	t.Parallel()

	for _, retainPool := range []bool{false, true} {
		t.Run(strconv.FormatBool(retainPool), func(t *testing.T) {
			t.Parallel()

			hzProvider, api := newAutoscalerHcloudAPI(t, removedPoolAndOtherClusterServers()...)

			pools := []string{}
			if retainPool {
				pools = append(pools, removedPool)
			}

			prov := disabledAutoscalerProvisioner(hzProvider, io.Discard, pools...)
			result := clusterupdate.NewEmptyUpdateResult()

			err := prov.AuditUpdate(t.Context(), autoscalerFakeCluster, result)
			require.NoError(t, err)

			reasons := failedChangeReasons(result)
			require.Len(t, reasons, 1)
			assert.Contains(t, reasons[0], "as-removed-1")
			assert.Contains(t, reasons[0], "node autoscaler is disabled")
			assert.NotContains(t, reasons[0], "other-pool-a")
			assert.Zero(t, api.unexpected.Load(), "inventory audit must never mutate a server")
		})
	}
}

func TestAuditUpdate_InventoryFailureIsNotSuccessfulAudit(t *testing.T) {
	t.Parallel()

	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			t.Parallel()

			hzProvider, api := newAutoscalerHcloudAPI(t)
			api.listStatus.Store(http.StatusForbidden)

			prov := disabledAutoscalerProvisioner(hzProvider, io.Discard)
			if enabled {
				prov = autoscalerProvisioner(hzProvider, io.Discard)
			}

			err := prov.AuditUpdate(
				t.Context(), autoscalerFakeCluster, clusterupdate.NewEmptyUpdateResult(),
			)
			require.ErrorContains(t, err, "listing autoscaler nodes")
			assert.Zero(t, api.unexpected.Load())
		})
	}
}

func TestAuditUpdate_NoHetznerOptionsRequiresNoInventory(t *testing.T) {
	t.Parallel()

	hzProvider, api := newAutoscalerHcloudAPI(t, removedPoolAndOtherClusterServers()...)
	prov := talosprovisioner.NewProvisioner(nil, nil).WithInfraProvider(hzProvider)
	result := clusterupdate.NewEmptyUpdateResult()

	err := prov.AuditUpdate(t.Context(), autoscalerFakeCluster, result)
	require.NoError(t, err)
	assert.Empty(t, result.FailedChanges)
	assert.Zero(t, api.unexpected.Load())
}
