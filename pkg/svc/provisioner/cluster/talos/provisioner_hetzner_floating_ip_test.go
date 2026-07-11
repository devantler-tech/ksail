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

// assertSavedKubeconfigServer runs the fetched-from-Talos kubeconfig fixture
// (talosFetchedKubeconfig, shared with connector_test.go) through the same
// rewrite saveHetznerKubeconfig performs and asserts the serialized file's
// server value — pinning the final artifact, not just the endpoint-URL helper.
func assertSavedKubeconfigServer(
	t *testing.T,
	provisioner *talosprovisioner.Provisioner,
	nodeIP string,
	wantServer string,
) {
	t.Helper()

	rewritten, err := talosprovisioner.RewriteKubeconfigEndpointForTest(
		[]byte(talosFetchedKubeconfig),
		provisioner.KubeconfigEndpointURLForTest(nodeIP),
	)
	require.NoError(t, err)
	assert.Contains(t, string(rewritten), "server: "+wantServer,
		"saved kubeconfig must target the effective cluster endpoint")
}

// assertClusterMetadataEndpoint pins HetznerClusterResult's connection
// metadata to the same effective endpoint the saved kubeconfig targets, so
// consumers of the metadata survive control-plane replacement too.
func assertClusterMetadataEndpoint(
	t *testing.T,
	provisioner *talosprovisioner.Provisioner,
	servers []*hcloud.Server,
	want string,
) {
	t.Helper()

	cluster, err := provisioner.NewHetznerClusterWithEndpointForTest(
		"fip-cluster", servers, nil,
	)
	require.NoError(t, err)
	assert.Equal(t, want, cluster.Info().KubernetesEndpoint,
		"connection metadata must expose the effective cluster endpoint")
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
	assert.Equal(t, "https://203.0.113.5:6443",
		provisioner.KubeconfigEndpointURLForTest("203.0.113.5"),
		"disabled path must save the kubeconfig against the node's reachable address")
	assertSavedKubeconfigServer(t, provisioner, "203.0.113.5", "https://203.0.113.5:6443")
	assertClusterMetadataEndpoint(t, provisioner,
		[]*hcloud.Server{controlPlaneServer(1, "cp-1", "203.0.113.5")},
		"https://203.0.113.5:6443")
}

// TestKubeconfigEndpointURL_FallsBackToNodeIP pins the defensive default: a
// provisioner that never rendered an endpoint (updateConfigsWithEndpoint not
// run) rewrites the kubeconfig to the dialed node's address.
func TestKubeconfigEndpointURL_FallsBackToNodeIP(t *testing.T) {
	t.Parallel()

	provisioner := newFloatingIPTestProvisioner(t, v1alpha1.OptionsHetzner{})

	assert.Equal(t, "https://203.0.113.7:6443",
		provisioner.KubeconfigEndpointURLForTest("203.0.113.7"))
	assertSavedKubeconfigServer(t, provisioner, "203.0.113.7", "https://203.0.113.7:6443")
}

// assertFloatingIPRenderedConfigs pins the enabled-path rendering: the
// floating IP as the cluster endpoint, the floating IP plus every
// control-plane node IP in the certificate SAN set, and a Talos VIP block
// (with hcloud API management) on the control-plane machine configs only —
// the node-side ownership handover that makes the elected leader claim the
// address on every leader change.
func assertFloatingIPRenderedConfigs(t *testing.T, configs *talos.Configs) {
	t.Helper()

	endpoint := configs.ControlPlane().Cluster().Endpoint()
	require.NotNil(t, endpoint)
	assert.Equal(t, "192.0.2.10", endpoint.Hostname(),
		"endpoint must be the floating IP")

	sans := configs.ControlPlane().Cluster().CertSANs()
	assert.Contains(t, sans, "192.0.2.10")
	assert.Contains(t, sans, "203.0.113.5")
	assert.Contains(t, sans, "203.0.113.6")

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

	assertFloatingIPRenderedConfigs(t, provisioner.TalosConfigsForTest())

	// The saved kubeconfig must target the stable floating-IP endpoint, not the
	// first control-plane node it was fetched from (#6043) — otherwise replacing
	// that cattle node still breaks every saved kubeconfig.
	assert.Equal(t, "https://192.0.2.10:6443",
		provisioner.KubeconfigEndpointURLForTest("203.0.113.5"),
		"kubeconfig endpoint must be the floating IP when the feature is enabled")
	assertSavedKubeconfigServer(t, provisioner, "203.0.113.5", "https://192.0.2.10:6443")
	assertClusterMetadataEndpoint(t, provisioner,
		[]*hcloud.Server{
			controlPlaneServer(1, "cp-1", "203.0.113.5"),
			controlPlaneServer(2, "cp-2", "203.0.113.6"),
		},
		"https://192.0.2.10:6443")
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
