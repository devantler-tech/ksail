package talosprovisioner_test

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	talos "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	hetzner "github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyHetznerDefaults_FloatingIPLocation covers the FloatingIPLocation
// default separately from TestApplyHetznerDefaults (same pattern as the
// FallbackLocations test): the floating IP is homed in the cluster's location
// unless explicitly overridden.
func TestApplyHetznerDefaults_FloatingIPLocation(t *testing.T) {
	t.Parallel()

	t.Run("defaults to the defaulted cluster location", func(t *testing.T) {
		t.Parallel()

		result := talosprovisioner.ApplyHetznerDefaultsForTest(v1alpha1.OptionsHetzner{})
		assert.Equal(t, v1alpha1.DefaultHetznerLocation, result.FloatingIPLocation)
	})

	t.Run("defaults to a custom cluster location", func(t *testing.T) {
		t.Parallel()

		result := talosprovisioner.ApplyHetznerDefaultsForTest(v1alpha1.OptionsHetzner{
			Location: "hel1",
		})
		assert.Equal(t, "hel1", result.FloatingIPLocation)
	})

	t.Run("preserves an explicit floating IP location", func(t *testing.T) {
		t.Parallel()

		result := talosprovisioner.ApplyHetznerDefaultsForTest(v1alpha1.OptionsHetzner{
			Location:           "hel1",
			FloatingIPLocation: "nbg1",
		})
		assert.Equal(t, "nbg1", result.FloatingIPLocation)
	})
}

// floatingIPEndpointTestServer answers the two hcloud API calls the enabled
// path makes: GET /floating_ips (GetByName) with an existing ksail-owned,
// unassigned floating IP, and POST /floating_ips/7/actions/assign counting
// assignments.
func floatingIPEndpointTestServer(t *testing.T, assignCalls *int32) *httptest.Server {
	t.Helper()

	const ownedFloatingIP = `{"id":7,"name":"fip-cluster-floating-ip","description":"",` +
		`"ip":"192.0.2.10","type":"ipv4","server":null,"dns_ptr":[],` +
		`"home_location":{"id":1,"name":"fsn1","description":"","country":"DE","city":"",` +
		`"latitude":0,"longitude":0,"network_zone":"eu-central"},` +
		`"blocked":false,"protection":{"delete":false},` +
		`"labels":{"ksail.owned":"true","ksail.cluster.name":"fip-cluster"},` +
		`"created":"2026-07-02T00:00:00+00:00"}`

	mux := http.NewServeMux()

	mux.HandleFunc(
		"/floating_ips",
		func(responseWriter http.ResponseWriter, request *http.Request) {
			responseWriter.Header().Set("Content-Type", "application/json")

			if request.Method != http.MethodGet {
				responseWriter.WriteHeader(http.StatusMethodNotAllowed)

				return
			}

			_, _ = responseWriter.Write([]byte(`{"floating_ips":[` + ownedFloatingIP + `]}`))
		},
	)

	mux.HandleFunc(
		"/floating_ips/7/actions/assign",
		func(responseWriter http.ResponseWriter, _ *http.Request) {
			responseWriter.Header().Set("Content-Type", "application/json")
			atomic.AddInt32(assignCalls, 1)

			body := map[string]any{
				"action": map[string]any{
					"id":       1,
					"command":  "assign_floating_ip",
					"status":   "success",
					"progress": 100,
				},
			}
			_ = json.NewEncoder(responseWriter).Encode(body)
		},
	)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server
}

// newFloatingIPTestProvisioner builds a provisioner with real generated Talos
// configs and the given Hetzner options, returning it for endpoint-rendering
// assertions.
func newFloatingIPTestProvisioner(
	t *testing.T,
	hetznerOpts v1alpha1.OptionsHetzner,
) *talosprovisioner.Provisioner {
	t.Helper()

	manager := talos.NewConfigManager(t.TempDir(), "fip-cluster", "1.32.0", "10.5.0.0/24")
	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, configs)

	return talosprovisioner.NewProvisioner(nil, nil).
		WithHetznerOptions(hetznerOpts).
		WithTalosConfigsForTest(configs).
		WithLogWriter(io.Discard)
}

func controlPlaneServer(id int64, name, publicIPv4 string) *hcloud.Server {
	return &hcloud.Server{
		ID:   id,
		Name: name,
		PublicNet: hcloud.ServerPublicNet{
			IPv4: hcloud.ServerPublicNetIPv4{IP: net.ParseIP(publicIPv4)},
		},
	}
}

// TestUpdateConfigsWithEndpoint_FloatingIPDisabled verifies the default path
// is unchanged: the endpoint is the first control-plane node's IP, no
// exposure cert-SANs patch is added, and the Hetzner API is never called (the
// provider has no client, so any call would error).
func TestUpdateConfigsWithEndpoint_FloatingIPDisabled(t *testing.T) {
	t.Parallel()

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{})

	err := provisioner.UpdateConfigsWithEndpointForTest(
		t.Context(),
		hetzner.NewProvider(nil),
		"fip-cluster",
		[]*hcloud.Server{controlPlaneServer(1, "cp-1", "203.0.113.5")},
	)
	require.NoError(t, err)

	configs := provisioner.TalosConfigsForTest()
	endpoint := configs.ControlPlane().Cluster().Endpoint()
	require.NotNil(t, endpoint)
	assert.Equal(t, "203.0.113.5", endpoint.Hostname())
	assert.NotContains(t, configs.ControlPlane().Cluster().CertSANs(), "192.0.2.10")
}

// TestUpdateConfigsWithEndpoint_FloatingIPEnabled verifies the opt-in path:
// the cluster's floating IP is attached to the first control-plane server and
// rendered as the endpoint, with the floating IP and every control-plane node
// IP in the certificate SAN set.
func TestUpdateConfigsWithEndpoint_FloatingIPEnabled(t *testing.T) {
	t.Parallel()

	var assignCalls int32

	server := floatingIPEndpointTestServer(t, &assignCalls)
	client := hcloud.NewClient(
		hcloud.WithToken("test-token"),
		hcloud.WithEndpoint(server.URL),
	)

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
	})

	err := provisioner.UpdateConfigsWithEndpointForTest(
		t.Context(),
		hetzner.NewProvider(client),
		"fip-cluster",
		[]*hcloud.Server{
			controlPlaneServer(1, "cp-1", "203.0.113.5"),
			controlPlaneServer(2, "cp-2", "203.0.113.6"),
		},
	)
	require.NoError(t, err)

	assert.Equal(t, int32(1), atomic.LoadInt32(&assignCalls),
		"floating IP must be attached to the first control-plane server")

	configs := provisioner.TalosConfigsForTest()
	endpoint := configs.ControlPlane().Cluster().Endpoint()
	require.NotNil(t, endpoint)
	assert.Equal(t, "192.0.2.10", endpoint.Hostname(),
		"endpoint must be the floating IP")

	sans := configs.ControlPlane().Cluster().CertSANs()
	assert.Contains(t, sans, "192.0.2.10")
	assert.Contains(t, sans, "203.0.113.5")
	assert.Contains(t, sans, "203.0.113.6")
}

// TestUpdateConfigsWithEndpoint_NoControlPlanes verifies the guard is
// preserved with the new signature.
func TestUpdateConfigsWithEndpoint_NoControlPlanes(t *testing.T) {
	t.Parallel()

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{})

	err := provisioner.UpdateConfigsWithEndpointForTest(
		t.Context(),
		hetzner.NewProvider(nil),
		"fip-cluster",
		nil,
	)
	require.Error(t, err)
}
