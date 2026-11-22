package kubernetes_test

import (
	"context"
	"encoding/json"
	"errors"
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
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
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

	require.NoError(t, otherProcess.RevokeNestedAPIGrant(t.Context(), "ksail-demo"))
	require.Len(t, cloud.rules, 2)
	assert.Equal(t, "6443", cloud.rules[0]["port"])
	assert.Equal(t, "31235", cloud.rules[1]["port"], "keep the sibling cluster's grant")
	require.NoError(t, otherProcess.RevokeNestedAPIGrant(t.Context(), "ksail-demo"))
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
	// failFirewallReads makes every firewall read fail, as an unreadable host firewall would.
	failFirewallReads bool
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

	if cloud.failFirewallReads {
		http.Error(writer, "forbidden", http.StatusForbidden)

		return
	}

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

	prov, _ := newAPIGrantProviderWithClient(t, cloud, providerIDs...)

	return prov
}

func newAPIGrantProviderWithClient(
	t *testing.T,
	cloud *apiGrantCloud,
	providerIDs ...string,
) (*kubeprovider.Provider, *fake.Clientset) {
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

	return prov.WithHetznerAPIFirewall(cloud.client), client
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
	require.NoError(t, afterJoin.RevokeNestedAPIGrant(t.Context(), "ksail-demo"))
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

// The grant is keyed by the namespace, which is unique on the host, so a same-named
// cluster of another distribution keeps its own grant.
func TestHetznerNodePortGrantIsKeyedByNamespace(t *testing.T) {
	t.Parallel()

	cloud := newAPIGrantCloud(t)
	prov := newAPIGrantProvider(t, cloud, "hcloud://42")
	_, err := prov.ResolveExposure(t.Context(), nil, apiGrantSpec())
	require.NoError(t, err)
	require.Len(t, cloud.rules, 3)
	assert.Equal(t, "KSail nested API: ksail-demo", cloud.rules[2]["description"])

	require.NoError(t, prov.RevokeNestedAPIGrant(t.Context(), "k3k-demo"))
	require.Len(t, cloud.rules, 3, "another namespace's revoke keeps this grant")
}

// A failed create of a cluster that already holds a grant must not revoke it: the attempt
// did not create that grant, and the running cluster still needs it.
func TestHetznerNodePortFailedDuplicateCreateKeepsExistingGrant(t *testing.T) {
	t.Parallel()

	cloud := newAPIGrantCloud(t)
	prov := newAPIGrantProvider(t, cloud, "hcloud://42")
	_, err := prov.ResolveExposure(t.Context(), nil, apiGrantSpec())
	require.NoError(t, err)

	duplicate, err := prov.ResolveExposure(t.Context(), nil, apiGrantSpec())
	require.NoError(t, err)

	failure := context.DeadlineExceeded
	require.ErrorIs(t, duplicate.CleanupOnFailure(t.Context(), failure), failure)
	require.Len(t, cloud.rules, 3, "the existing cluster's grant is kept")
}

// Deletion without a Hetzner credential stops on a host whose nodes are Hetzner servers,
// because only the firewall can tell whether a grant exists, and stays a no-op elsewhere.
func TestHetznerNodePortRevokeWithoutCredentialRefusesHetznerHost(t *testing.T) {
	t.Parallel()

	cloud := newAPIGrantCloud(t)
	prov := newAPIGrantProvider(t, cloud, "hcloud://42")
	_, err := prov.ResolveExposure(t.Context(), nil, apiGrantSpec())
	require.NoError(t, err)

	prov.WithHetznerAPIFirewall(nil)
	err = prov.RevokeNestedAPIGrant(t.Context(), "ksail-demo")
	require.ErrorIs(t, err, kubeprovider.ErrHetznerGrantCredentials)
	require.Len(t, cloud.rules, 3)

	// A Hetzner host that never granted this namespace still needs the credential to prove it.
	require.ErrorIs(t,
		prov.RevokeNestedAPIGrant(t.Context(), "k3k-demo"),
		kubeprovider.ErrHetznerGrantCredentials)

	other := newAPIGrantProvider(t, cloud, "aws:///zone/i-123", "").WithHetznerAPIFirewall(nil)
	require.NoError(t, other.RevokeNestedAPIGrant(t.Context(), "ksail-demo"))
}

// The firewall alone records a grant: rebuilding the API Service without a credential
// leaves the grant in place, and a later credentialed delete still removes it.
func TestHetznerNodePortServiceRebuildKeepsGrantRevocable(t *testing.T) {
	t.Parallel()

	cloud := newAPIGrantCloud(t)
	prov := newAPIGrantProvider(t, cloud, "hcloud://42")
	_, err := prov.ResolveExposure(t.Context(), nil, apiGrantSpec())
	require.NoError(t, err)

	prov.WithHetznerAPIFirewall(nil)
	_, err = prov.ResolveExposure(t.Context(), nil, apiGrantSpec())
	require.NoError(t, err)
	require.Len(t, cloud.rules, 3)

	prov.WithHetznerAPIFirewall(cloud.client)
	require.NoError(t, prov.RevokeNestedAPIGrant(t.Context(), "ksail-demo"))
	require.Len(t, cloud.rules, 2)
}

var errGatewayUnavailable = errors.New("gateway unavailable")

// After compensation removed a failed attempt's grant, a retry's new grant is its own, so
// a failed retry removes it too.
func TestHetznerNodePortFailedRetryRemovesItsOwnGrant(t *testing.T) {
	t.Parallel()

	cloud := newAPIGrantCloud(t)
	prov := newAPIGrantProvider(t, cloud, "hcloud://42")
	result, err := prov.ResolveExposure(t.Context(), nil, apiGrantSpec())
	require.NoError(t, err)

	failure := context.DeadlineExceeded
	require.ErrorIs(t, result.CleanupOnFailure(t.Context(), failure), failure)
	require.Len(t, cloud.rules, 2)

	retry, err := prov.ResolveExposure(t.Context(), nil, apiGrantSpec())
	require.NoError(t, err)
	require.Len(t, cloud.rules, 3)
	require.ErrorIs(t, retry.CleanupOnFailure(t.Context(), failure), failure)
	require.Len(t, cloud.rules, 2, "a failed retry removes its own grant")
}

// An unreadable host firewall stops exposure before any grant, so no rule is written
// without knowing whether it predates the attempt.
func TestHetznerNodePortUnreadableFirewallStopsExposure(t *testing.T) {
	t.Parallel()

	cloud := newAPIGrantCloud(t)
	cloud.failFirewallReads = true
	prov := newAPIGrantProvider(t, cloud, "hcloud://42")

	_, err := prov.ResolveExposure(t.Context(), nil, apiGrantSpec())
	require.Error(t, err)
	assert.Zero(t, cloud.writes)
}

// Moving the API to a load balancer removes the NodePort grant once clients reach the
// load balancer, so the port is not left admitted after another Service takes it.
func TestHetznerNodePortGrantRemovedWhenLoadBalancerTakesOver(t *testing.T) {
	t.Parallel()

	cloud := newAPIGrantCloud(t)
	prov, client := newAPIGrantProviderWithClient(t, cloud, "hcloud://42")
	_, err := prov.ResolveExposure(t.Context(), nil, apiGrantSpec())
	require.NoError(t, err)
	require.Len(t, cloud.rules, 3)

	client.PrependReactor("update", "services",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			update, _ := action.(k8stesting.UpdateAction)
			svc, _ := update.GetObject().(*corev1.Service)
			svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "203.0.113.5"}}

			return false, nil, nil
		})

	spec := apiGrantSpec()
	spec.SkipLoadBalancer = false
	result, err := prov.ResolveExposure(t.Context(), nil, spec)
	require.NoError(t, err)
	assert.Equal(t, kubeprovider.ExposureLoadBalancer, result.Kind)
	require.Len(t, cloud.rules, 2, "the NodePort grant is removed")
}

// A Gateway attempt removes the grant before its ClusterIP Service releases the NodePort.
// When the Gateway then fails and resolution falls back to NodePort, the re-created grant
// belongs to the running cluster, so a failed duplicate create must keep it.
func TestHetznerNodePortGatewayFallbackKeepsRunningClusterGrant(t *testing.T) {
	t.Parallel()

	cloud := newAPIGrantCloud(t)
	prov, client := newAPIGrantProviderWithClient(t, cloud, "hcloud://42")
	_, err := prov.ResolveExposure(t.Context(), nil, apiGrantSpec())
	require.NoError(t, err)

	// The fake clientset does not allocate node ports; the API server re-allocates one
	// when the Service returns to NodePort.
	client.PrependReactor("update", "services",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			update, _ := action.(k8stesting.UpdateAction)

			svc, _ := update.GetObject().(*corev1.Service)
			if svc.Spec.Type == corev1.ServiceTypeNodePort {
				svc.Spec.Ports[0].NodePort = 31234
			}

			return false, nil, nil
		})

	gateway := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	gateway.PrependReactor("*", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errGatewayUnavailable
	})

	spec := apiGrantSpec()
	spec.GatewayClassName = "unavailable"
	duplicate, err := prov.ResolveExposure(t.Context(), gateway, spec)
	require.NoError(t, err)
	require.Equal(t, kubeprovider.ExposureNodePort, duplicate.Kind)
	require.Len(t, cloud.rules, 3, "the fallback grants the NodePort again")

	failure := context.DeadlineExceeded
	require.ErrorIs(t, duplicate.CleanupOnFailure(t.Context(), failure), failure)
	require.Len(t, cloud.rules, 3, "the running cluster keeps its grant")
}
