package talosprovisioner_test

import (
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

// Env-var NAMES the floating-IP tests use for the hcloud token lookup — names,
// not credentials.
const (
	testFloatingIPTokenEnvVar      = "KSAIL_TEST_HCLOUD_TOKEN_FIP"       //nolint:gosec // env var name, not a credential
	testFloatingIPTokenEnvVarUnset = "KSAIL_TEST_HCLOUD_TOKEN_FIP_UNSET" //nolint:gosec // env var name, not a credential
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

	mux := http.NewServeMux()

	mux.HandleFunc(
		"/floating_ips",
		func(responseWriter http.ResponseWriter, request *http.Request) {
			responseWriter.Header().Set("Content-Type", "application/json")

			if request.Method != http.MethodGet {
				responseWriter.WriteHeader(http.StatusMethodNotAllowed)

				return
			}

			// The canned owned floating IP is shared with the update-reconcile
			// tests (update_floating_ip_test.go).
			_, _ = responseWriter.Write(
				[]byte(`{"floating_ips":[` + fipUpdateOwnedFloatingIPJSON + `]}`),
			)
		},
	)

	mux.HandleFunc(
		"/floating_ips/7/actions/assign",
		func(responseWriter http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(assignCalls, 1)
			fipUpdateAssignActionResponse(responseWriter)
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
	assert.Empty(t, configs.ControlPlane().Machine().Network().Devices(),
		"disabled floating IP must render no VIP interface block")
}

// TestUpdateConfigsWithEndpoint_FloatingIPEnabled verifies the opt-in path:
// the cluster's floating IP is attached to the first control-plane server and
// rendered as the endpoint, with the floating IP and every control-plane node
// IP in the certificate SAN set, and a Talos VIP block on the control-plane
// machine configs so the elected leader owns the address (no t.Parallel:
// t.Setenv provides the hcloud token the VIP block embeds).
func TestUpdateConfigsWithEndpoint_FloatingIPEnabled(t *testing.T) {
	t.Setenv(testFloatingIPTokenEnvVar, "vip-test-token")

	var assignCalls int32

	server := floatingIPEndpointTestServer(t, &assignCalls)
	client := hcloud.NewClient(
		hcloud.WithToken("test-token"),
		hcloud.WithEndpoint(server.URL),
	)

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{
		FloatingIPEnabled:  true,
		FloatingIPLocation: "fsn1",
		TokenEnvVar:        testFloatingIPTokenEnvVar,
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

	// Node-side ownership handover: the control-plane configs must carry the
	// Talos VIP block for the floating IP, with hcloud API management.
	devices := configs.ControlPlane().Machine().Network().Devices()
	require.Len(t, devices, 1)
	assert.Equal(t, "eth0", devices[0].Interface())

	vip := devices[0].VIPConfig()
	require.NotNil(t, vip)
	assert.Equal(t, "192.0.2.10", vip.IP())
	require.NotNil(t, vip.HCloud())
	assert.Equal(t, "vip-test-token", vip.HCloud().APIToken())

	// Workers claim nothing: the VIP patch is control-plane-scoped.
	assert.Empty(t, configs.Worker().Machine().Network().Devices())
}

// TestUpdateConfigsWithEndpoint_FloatingIPEnabledTokenUnset verifies the
// enabled path fails loudly when the hcloud token env var is unset — the VIP
// block cannot manage the floating IP without it — and fails FAST: the token
// is validated before any hcloud call, so no floating IP is ensured or
// attached on the way to the error.
func TestUpdateConfigsWithEndpoint_FloatingIPEnabledTokenUnset(t *testing.T) {
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
		TokenEnvVar:        testFloatingIPTokenEnvVarUnset,
	})

	err := provisioner.UpdateConfigsWithEndpointForTest(
		t.Context(),
		hetzner.NewProvider(client),
		"fip-cluster",
		[]*hcloud.Server{controlPlaneServer(1, "cp-1", "203.0.113.5")},
	)
	require.ErrorIs(t, err, talosprovisioner.ErrHcloudTokenNotSet)
	assert.Equal(t, int32(0), atomic.LoadInt32(&assignCalls),
		"missing token must fail before any hcloud assign call")
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
