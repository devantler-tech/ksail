package kubernetes_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	kubeprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// Removing the grant or using the service's target port instead of its allocated
// NodePort must fail this test. The real Hetzner rule writer talks only to httptest.
func TestHetznerNodePortGrantLifecycle(t *testing.T) {
	t.Parallel()

	cloud := newAPIGrantCloud(t)
	prov := newAPIGrantProvider(t, cloud, "hcloud://42")
	result, err := prov.ResolveExposure(t.Context(), nil, apiGrantSpec())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, int32(31234), result.Port)
	require.Len(t, cloud.rules, 3)
	grant := cloud.rules[2]
	assert.Equal(t, "31234", grant["port"])
	assert.Equal(t, []any{"192.0.2.7/32"}, grant["source_ips"])
	assert.Equal(t, "in", grant["direction"])

	// A successful create retains the grant; a later process can rediscover and
	// remove it without relying on in-memory state from creation.
	var createErr error

	createErr = result.CleanupOnFailure(t.Context(), createErr)
	require.NoError(t, createErr)
	require.Len(t, cloud.rules, 3)
	otherProcess := newAPIGrantProvider(t, cloud, "hcloud://42")
	cloud.unattached = true

	require.NoError(t, otherProcess.RevokeNestedAPIGrant(t.Context(), "demo"))
	require.Len(t, cloud.rules, 2)
	assert.Equal(t, "6443", cloud.rules[0]["port"])
	assert.Equal(t, "31235", cloud.rules[1]["port"], "keep the sibling cluster's grant")
	require.NoError(t, otherProcess.RevokeNestedAPIGrant(t.Context(), "demo"))
}

func TestHetznerNodePortFailedCreateRevokesWithCancelledContext(t *testing.T) {
	t.Parallel()

	cloud := newAPIGrantCloud(t)
	prov := newAPIGrantProvider(t, cloud, "hcloud://42")
	ctx, cancel := context.WithCancel(t.Context())
	result, err := prov.ResolveExposure(ctx, nil, apiGrantSpec())
	require.NoError(t, err)
	cancel()

	failure := context.DeadlineExceeded
	createErr := failure
	createErr = result.CleanupOnFailure(ctx, createErr)
	require.ErrorIs(t, createErr, failure)
	require.Len(t, cloud.rules, 2, "cancelled readiness must not strand the port grant")
}

func TestHetznerNodePortOwnershipBoundary(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		providerID string
		unowned    bool
		unattached bool
		wantError  bool
	}{
		{name: "other provider", providerID: "aws:///zone/i-123"},
		{name: "unidentified host", providerID: ""},
		{name: "malformed Hetzner identity", providerID: "hcloud://42/other", wantError: true},
		{name: "unowned server", providerID: "hcloud://42", unowned: true},
		{name: "firewall not attached", providerID: "hcloud://42", unattached: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cloud := newAPIGrantCloud(t)
			cloud.unowned = testCase.unowned
			cloud.unattached = testCase.unattached
			prov := newAPIGrantProvider(t, cloud, testCase.providerID)

			result, err := prov.ResolveExposure(t.Context(), nil, apiGrantSpec())
			if testCase.wantError {
				require.Error(t, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, int32(31234), result.Port)
			}

			assert.Zero(t, cloud.writes, "unproven ownership never authorizes a firewall write")
		})
	}
}

type apiGrantCloud struct {
	client     *hcloud.Client
	rules      []map[string]any
	writes     int
	unowned    bool
	unattached bool
}

func newAPIGrantCloud(t *testing.T) *apiGrantCloud {
	t.Helper()

	cloud := &apiGrantCloud{rules: []map[string]any{
		{
			"direction": "in", "protocol": "tcp", "port": "6443",
			"source_ips": []string{"192.0.2.7/32"}, "description": "Kubernetes API",
		},
		{
			"direction": "in", "protocol": "tcp", "port": "31235",
			"source_ips": []string{"192.0.2.7/32"}, "description": "KSail nested API: sibling",
		},
	}}
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/json")

			switch {
			case request.Method == http.MethodGet && request.URL.Path == "/servers/42":
				cloud.writeHostServer(t, writer)
			case request.Method == http.MethodGet && request.URL.Path == "/firewalls":
				cloud.writeFirewalls(t, writer)
			case request.Method == http.MethodPost && request.URL.Path == "/firewalls/7/actions/set_rules":
				var payload map[string][]map[string]any

				err := json.NewDecoder(request.Body).Decode(&payload)
				if err != nil {
					http.Error(writer, "invalid rules", http.StatusBadRequest)

					return
				}

				cloud.rules = payload["rules"]
				cloud.writes++

				assert.NoError(
					t,
					json.NewEncoder(writer).Encode(map[string]any{"actions": []any{}}),
				)
			default:
				http.Error(writer, "unexpected cloud request", http.StatusBadRequest)
			}
		}),
	)
	t.Cleanup(server.Close)

	cloud.client = hcloud.NewClient(hcloud.WithToken("fixture"), hcloud.WithEndpoint(server.URL))

	return cloud
}

func (cloud *apiGrantCloud) writeFirewalls(t *testing.T, writer http.ResponseWriter) {
	t.Helper()

	assert.NoError(t, json.NewEncoder(writer).Encode(map[string]any{"firewalls": []map[string]any{{
		"id": 7, "name": "host-firewall",
		"labels": map[string]string{"ksail.owned": "true", "ksail.cluster.name": "host"},
		"rules":  cloud.rules,
	}}}))
}

func (cloud *apiGrantCloud) writeHostServer(t *testing.T, writer http.ResponseWriter) {
	t.Helper()

	labels := map[string]string{"ksail.owned": "true", "ksail.cluster.name": "host"}
	if cloud.unowned {
		delete(labels, "ksail.owned")
	}

	firewalls := []map[string]any{{"id": 7, "status": "applied"}}
	if cloud.unattached {
		firewalls = nil
	}

	assert.NoError(t, json.NewEncoder(writer).Encode(map[string]any{"server": map[string]any{
		"id": 42, "name": "host-node", "labels": labels,
		"public_net": map[string]any{"firewalls": firewalls},
	}}))
}

func newAPIGrantProvider(
	t *testing.T,
	cloud *apiGrantCloud,
	providerIDs ...string,
) *kubeprovider.Provider {
	t.Helper()

	objects := make([]runtime.Object, 0, len(providerIDs)+1)

	for index, providerID := range providerIDs {
		node := nodeWithAddresses(
			corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "198.51.100.9"},
		)
		node.Name = "n" + strconv.Itoa(index+1)
		node.Spec.ProviderID = providerID
		objects = append(objects, node)
	}

	objects = append(objects, &corev1.Service{
		Name:      kubeprovider.APIServiceName,
		Namespace: "ksail-demo",
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Name: "https", Port: 6443, NodePort: 31234}},
		},
	})
	client := fake.NewClientset(objects...)
	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	return prov.WithHetznerAPIFirewall(cloud.client)
}

func apiGrantSpec() kubeprovider.APIExposureSpec {
	return kubeprovider.APIExposureSpec{
		ClusterName: "demo", APIPort: 6443, SkipLoadBalancer: true,
		HostAddress: "https://198.51.100.9:6443",
	}
}

// A node that joined after the grant and has no Hetzner identity yet must not
// turn revocation into a silent no-op that leaves the nested API port open.
func TestHetznerNodePortRevokeIgnoresUnidentifiedJoiningNode(t *testing.T) {
	t.Parallel()

	cloud := newAPIGrantCloud(t)
	prov := newAPIGrantProvider(t, cloud, "hcloud://42")
	_, err := prov.ResolveExposure(t.Context(), nil, apiGrantSpec())
	require.NoError(t, err)
	require.Len(t, cloud.rules, 3)

	afterJoin := newAPIGrantProvider(t, cloud, "hcloud://42", "")
	require.NoError(t, afterJoin.RevokeNestedAPIGrant(t.Context(), "demo"))
	require.Len(t, cloud.rules, 2, "revoke must remove the grant despite a joining node")
	assert.Equal(t, "31235", cloud.rules[1]["port"], "keep the sibling cluster's grant")
}

// A grant still needs every node to be a proven Hetzner server.
func TestHetznerNodePortGrantRefusesMixedNodes(t *testing.T) {
	t.Parallel()

	cloud := newAPIGrantCloud(t)
	prov := newAPIGrantProvider(t, cloud, "hcloud://42", "")
	_, err := prov.ResolveExposure(t.Context(), nil, apiGrantSpec())
	require.NoError(t, err)
	assert.Zero(t, cloud.writes, "a node without a Hetzner identity never authorizes a grant")
}
