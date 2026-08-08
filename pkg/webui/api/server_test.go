package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const defaultNS = "default"

func newClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(&v1alpha1.Cluster{}).
		Build()
}

func sampleCluster() *v1alpha1.Cluster {
	return &v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: defaultNS},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{Distribution: v1alpha1.DistributionVCluster},
		},
	}
}

func doRequest(handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(
		context.Background(),
		method,
		target,
		strings.NewReader(body),
	)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	return recorder
}

func TestConfigReportsReadOnly(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: operator.NewCRClusterService(newClient(t)), ReadOnly: true}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(
		t,
		`{"readOnly":true,"authEnabled":false,`+
			`"capabilities":{"clusterUpdate":true,"workloadRead":false,`+
			`"workloadWrite":false,"kubeconfigDownload":false,`+
			`"applyManifests":false,"secretsCipher":false,"workloadLogs":false,"workloadExec":false,`+
			`"clusterStartStop":false,"componentsInstall":true,"plugins":false,`+
			`"aiChat":false,"kubeProxy":false,"pluginInstall":false,"aiChatWrite":false,"pluginCatalog":false,`+
			`"kubeWatch":false,"wsMultiplexer":false}}`,
		recorder.Body.String(),
	)
}

func TestListClusters(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: operator.NewCRClusterService(newClient(t, sampleCluster()))}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/clusters", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "c1")
}

// stubClusterService returns a fixed cluster list without any controller-runtime round-trip, so a
// test can assert exactly what the REST layer serializes (the fake client prunes via MarshalJSON on
// storage, which would mask the serialization being tested here).
type stubClusterService struct {
	clusters []v1alpha1.Cluster
}

func (s stubClusterService) List(_ context.Context) (*v1alpha1.ClusterList, error) {
	return &v1alpha1.ClusterList{Items: s.clusters}, nil
}

func (s stubClusterService) Get(_ context.Context, _, _ string) (*v1alpha1.Cluster, error) {
	return nil, api.ErrNotFound
}

func (s stubClusterService) Create(
	_ context.Context,
	cluster *v1alpha1.Cluster,
) (*v1alpha1.Cluster, error) {
	return cluster, nil
}

func (s stubClusterService) Delete(_ context.Context, _, _ string) error {
	return nil
}

// updaterStub is a ClusterService that also implements api.ClusterUpdater, so a test can assert that
// implementing the optional interface flips capabilities.clusterUpdate to true on /api/v1/config (the
// way the operator backend advertises it).
type updaterStub struct {
	stubClusterService
}

func (updaterStub) Update(
	_ context.Context,
	_, _ string,
	cluster *v1alpha1.Cluster,
) (*v1alpha1.Cluster, error) {
	return cluster, nil
}

func TestListClustersIncludesDefaultValuedFields(t *testing.T) {
	t.Parallel()

	vanilla := v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "van", Namespace: defaultNS},
		Spec: v1alpha1.Spec{Cluster: v1alpha1.ClusterSpec{
			Distribution: v1alpha1.DistributionVanilla,
			Provider:     v1alpha1.ProviderDocker,
		}},
	}
	server := &api.Server{Service: stubClusterService{clusters: []v1alpha1.Cluster{vanilla}}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/clusters", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	// The REST API must surface default-valued fields so the UI can display them, unlike the
	// config-file marshaler (v1alpha1.Cluster.MarshalJSON) that prunes defaults like Vanilla/Docker.
	assert.Contains(t, recorder.Body.String(), `"distribution":"Vanilla"`)
	assert.Contains(t, recorder.Body.String(), `"provider":"Docker"`)
}

func TestListClustersEmptyReturnsArray(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: operator.NewCRClusterService(newClient(t))}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/clusters", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	// Empty list must serialize items as [] (not null) to match Kubernetes list semantics.
	assert.Contains(t, recorder.Body.String(), `"items":[]`)
}

func TestGetClusterNotFound(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: operator.NewCRClusterService(newClient(t))}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/clusters/default/missing", "")

	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestCreateClusterOversizedBodyReturns413(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: operator.NewCRClusterService(newClient(t))}
	// Body exceeds the 1 MiB cap, so the decode must surface 413, not a generic 400.
	body := `{"metadata":{"name":"big","namespace":"default","labels":{"big":"` +
		strings.Repeat("a", 1<<20) + `"}}}`

	recorder := doRequest(server.Handler(), http.MethodPost, "/api/v1/clusters", body)

	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

func TestCreateClusterRejectsSimpleCrossOriginContentType(t *testing.T) {
	t.Parallel()

	fakeClient := newClient(t)
	server := &api.Server{Service: operator.NewCRClusterService(fakeClient)}
	body := `{"metadata":{"name":"evil-eks","namespace":"default"},"spec":{"cluster":{"distribution":"EKS","provider":"AWS"}}}`
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/clusters",
		strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "text/plain")
	request.Header.Set("Origin", "https://attacker.example")

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusUnsupportedMediaType, recorder.Code)

	err := fakeClient.Get(
		context.Background(),
		client.ObjectKey{Namespace: defaultNS, Name: "evil-eks"},
		&v1alpha1.Cluster{},
	)
	assert.Error(t, err, "cluster should not be created from a simple cross-origin content type")
}

func TestCreateClusterWhenWritable(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: operator.NewCRClusterService(newClient(t))}
	body := `{"metadata":{"name":"new","namespace":"default"},"spec":{"cluster":{"distribution":"VCluster"}}}`

	recorder := doRequest(server.Handler(), http.MethodPost, "/api/v1/clusters", body)

	assert.Equal(t, http.StatusCreated, recorder.Code)
}

// readOnlyBody is the exact 403 read-only rejection body — a wire contract: the SPA parses the
// "reason" key (web/ui/src/api.ts detailFromBody), so every read-only rejection (the middleware
// guard and the exec handler's self-check) must emit it byte-identically.
const readOnlyBody = `{"readOnly":true,"reason":"UI is configured read-only (GitOps-enforced)"}`

func TestReadOnlyRejectsCreate(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: operator.NewCRClusterService(newClient(t)), ReadOnly: true}
	body := `{"metadata":{"name":"new","namespace":"default"}}`

	recorder := doRequest(server.Handler(), http.MethodPost, "/api/v1/clusters", body)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	//nolint:testifylint // assert the exact bytes: the body is a wire contract, JSON-equivalence is too weak
	assert.Equal(t, readOnlyBody, recorder.Body.String())
}

func TestReadOnlyRejectsDelete(t *testing.T) {
	t.Parallel()

	server := &api.Server{
		Service:  operator.NewCRClusterService(newClient(t, sampleCluster())),
		ReadOnly: true,
	}

	recorder := doRequest(server.Handler(), http.MethodDelete, "/api/v1/clusters/default/c1", "")

	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestReadOnlyAllowsReads(t *testing.T) {
	t.Parallel()

	server := &api.Server{
		Service:  operator.NewCRClusterService(newClient(t, sampleCluster())),
		ReadOnly: true,
	}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/clusters", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestDeleteClusterWhenWritable(t *testing.T) {
	t.Parallel()

	fakeClient := newClient(t, sampleCluster())
	server := &api.Server{Service: operator.NewCRClusterService(fakeClient)}

	recorder := doRequest(server.Handler(), http.MethodDelete, "/api/v1/clusters/default/c1", "")

	assert.Equal(t, http.StatusNoContent, recorder.Code)

	err := fakeClient.Get(
		context.Background(),
		client.ObjectKey{Namespace: defaultNS, Name: "c1"},
		&v1alpha1.Cluster{},
	)
	assert.Error(t, err, "cluster should be deleted")
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: operator.NewCRClusterService(newClient(t))}

	recorder := doRequest(server.Handler(), http.MethodGet, "/healthz", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestUpdateClusterAppliesSpec(t *testing.T) {
	t.Parallel()

	fakeClient := newClient(t, sampleCluster())
	server := &api.Server{Service: operator.NewCRClusterService(fakeClient)}
	body := `{"spec":{"cluster":{"distribution":"K3s"}}}`

	recorder := doRequest(server.Handler(), http.MethodPut, "/api/v1/clusters/default/c1", body)
	assert.Equal(t, http.StatusOK, recorder.Code)

	var got v1alpha1.Cluster

	require.NoError(t, fakeClient.Get(
		context.Background(),
		client.ObjectKey{Namespace: defaultNS, Name: "c1"},
		&got,
	))
	assert.Equal(t, v1alpha1.DistributionK3s, got.Spec.Cluster.Distribution)
}

func TestUpdateClusterNotFound(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: operator.NewCRClusterService(newClient(t))}
	body := `{"spec":{"cluster":{"distribution":"VCluster"}}}`

	recorder := doRequest(
		server.Handler(),
		http.MethodPut,
		"/api/v1/clusters/default/missing",
		body,
	)
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestCreateDefaultsNamespace(t *testing.T) {
	t.Parallel()

	fakeClient := newClient(t)
	server := &api.Server{Service: operator.NewCRClusterService(fakeClient)}
	body := `{"metadata":{"name":"nons"},"spec":{"cluster":{"distribution":"VCluster"}}}`

	recorder := doRequest(server.Handler(), http.MethodPost, "/api/v1/clusters", body)
	assert.Equal(t, http.StatusCreated, recorder.Code)

	err := fakeClient.Get(
		context.Background(),
		client.ObjectKey{Namespace: defaultNS, Name: "nons"},
		&v1alpha1.Cluster{},
	)
	assert.NoError(t, err, "cluster should be created in the default namespace")
}

func TestCreateSanitizesClientInput(t *testing.T) {
	t.Parallel()

	fakeClient := newClient(t)
	server := &api.Server{Service: operator.NewCRClusterService(fakeClient)}
	// Client tries to set operator-managed fields; they must be stripped.
	body := `{"metadata":{"name":"san","namespace":"default",` +
		`"finalizers":["ksail.io/finalizer"],` +
		`"annotations":{"ksail.io/last-applied-spec":"{}","keep":"yes"}},` +
		`"spec":{"cluster":{"distribution":"VCluster"}}}`

	recorder := doRequest(server.Handler(), http.MethodPost, "/api/v1/clusters", body)
	require.Equal(t, http.StatusCreated, recorder.Code)

	var got v1alpha1.Cluster

	require.NoError(t, fakeClient.Get(
		context.Background(),
		client.ObjectKey{Namespace: defaultNS, Name: "san"},
		&got,
	))
	assert.Empty(t, got.Finalizers, "finalizers must be stripped")
	assert.NotContains(
		t,
		got.Annotations,
		"ksail.io/last-applied-spec",
		"operator annotation stripped",
	)
	assert.Equal(t, "yes", got.Annotations["keep"], "non-operator annotations preserved")
}

func TestCreateConflictReturns409(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: operator.NewCRClusterService(newClient(t, sampleCluster()))}
	body := `{"metadata":{"name":"c1","namespace":"default"},"spec":{"cluster":{"distribution":"VCluster"}}}`

	recorder := doRequest(server.Handler(), http.MethodPost, "/api/v1/clusters", body)
	assert.Equal(t, http.StatusConflict, recorder.Code)
}

func TestConfigDefaultsWritable(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: operator.NewCRClusterService(newClient(t))}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(
		t,
		`{"readOnly":false,"authEnabled":false,`+
			`"capabilities":{"clusterUpdate":true,"workloadRead":false,`+
			`"workloadWrite":false,"kubeconfigDownload":false,`+
			`"applyManifests":false,"secretsCipher":false,"workloadLogs":false,"workloadExec":false,`+
			`"clusterStartStop":false,"componentsInstall":true,"plugins":false,`+
			`"aiChat":false,"kubeProxy":false,"pluginInstall":false,"aiChatWrite":false,"pluginCatalog":false,`+
			`"kubeWatch":false,"wsMultiplexer":false}}`,
		recorder.Body.String(),
	)
}

func TestConfigReportsNoClusterUpdateWithoutUpdater(t *testing.T) {
	t.Parallel()

	// A backend that does not implement ClusterUpdater (the local UI/desktop backend) reports
	// clusterUpdate=false, derived from the interface like the other flags, so the SPA hides the edit
	// affordance rather than offering an action that returns 501.
	server := &api.Server{Service: stubClusterService{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(
		t,
		recorder.Body.String(),
		`"capabilities":{"clusterUpdate":false,"workloadRead":false,`+
			`"workloadWrite":false,"kubeconfigDownload":false,`+
			`"applyManifests":false,"secretsCipher":false,"workloadLogs":false,"workloadExec":false,`+
			`"clusterStartStop":false,"componentsInstall":false,"plugins":false,`+
			`"aiChat":false,"kubeProxy":false,"pluginInstall":false,"aiChatWrite":false,"pluginCatalog":false,`+
			`"kubeWatch":false,"wsMultiplexer":false}`,
	)
}

func TestConfigReportsClusterUpdateWithUpdater(t *testing.T) {
	t.Parallel()

	// A backend that implements ClusterUpdater (the operator's CR backend, and this updaterStub)
	// advertises clusterUpdate=true — the same interface-derived mechanism as the other capabilities.
	server := &api.Server{Service: updaterStub{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(
		t,
		recorder.Body.String(),
		`"capabilities":{"clusterUpdate":true,"workloadRead":false,`+
			`"workloadWrite":false,"kubeconfigDownload":false,`+
			`"applyManifests":false,"secretsCipher":false,"workloadLogs":false,"workloadExec":false,`+
			`"clusterStartStop":false,"componentsInstall":false,"plugins":false,`+
			`"aiChat":false,"kubeProxy":false,"pluginInstall":false,"aiChatWrite":false,"pluginCatalog":false,`+
			`"kubeWatch":false,"wsMultiplexer":false}`,
	)
}

func TestConfigReportsClusterUpdateForOperatorBackend(t *testing.T) {
	t.Parallel()

	// The operator's CR backend implements ClusterUpdater (it patches the Cluster CR), so clusterUpdate
	// is true.
	server := &api.Server{Service: operator.NewCRClusterService(newClient(t))}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(
		t,
		recorder.Body.String(),
		`"capabilities":{"clusterUpdate":true,"workloadRead":false,`+
			`"workloadWrite":false,"kubeconfigDownload":false,`+
			`"applyManifests":false,"secretsCipher":false,"workloadLogs":false,"workloadExec":false,`+
			`"clusterStartStop":false,"componentsInstall":true,"plugins":false,`+
			`"aiChat":false,"kubeProxy":false,"pluginInstall":false,"aiChatWrite":false,"pluginCatalog":false,`+
			`"kubeWatch":false,"wsMultiplexer":false}`,
	)
}

// TestConfigReportsClusterUpdateForConnectedOperatorBackend pins that the production connected operator
// backend (resource browser enabled) also implements ClusterUpdater — via its embedded crClusterService
// — and the resource capabilities derive from the embedded ResourceAdapter, so the config reports
// clusterUpdate=true alongside workloadRead/workloadWrite=true.
func TestConfigReportsClusterUpdateForConnectedOperatorBackend(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: newConnectedService(t, nil)}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, `"clusterUpdate":true`)
	assert.Contains(t, body, `"workloadRead":true`)
	assert.Contains(t, body, `"workloadWrite":true`)
}

// TestUpdateClusterNotSupportedReturns501 covers the local backend's PUT: a backend without
// ClusterUpdater returns 501 with the message detail the deleted local stub carried, so the SPA's
// error surface is unchanged.
func TestUpdateClusterNotSupportedReturns501(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: stubClusterService{}}
	body := `{"spec":{"cluster":{"distribution":"K3s"}}}`

	recorder := doRequest(server.Handler(), http.MethodPut, "/api/v1/clusters/default/c1", body)

	assert.Equal(t, http.StatusNotImplemented, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "updating clusters is not supported locally")
}

// lifecycleStub is a ClusterService that also implements api.ClusterLifecycleController, recording the
// start/stop calls it receives so a test can assert the routes reach the backend.
type lifecycleStub struct {
	stubClusterService

	started []string
	stopped []string
}

func (s *lifecycleStub) Start(_ context.Context, _, name string) error {
	s.started = append(s.started, name)

	return nil
}

func (s *lifecycleStub) Stop(_ context.Context, _, name string) error {
	s.stopped = append(s.stopped, name)

	return nil
}

// TestConfigReportsClusterStartStop pins that a backend implementing ClusterLifecycleController
// advertises clusterStartStop=true (the interface-derived gate), while one that does not reports
// false — so the SPA only offers start/stop where the routes actually exist.
func TestConfigReportsClusterStartStop(t *testing.T) {
	t.Parallel()

	withStartStop := &api.Server{Service: &lifecycleStub{}}
	without := &api.Server{Service: stubClusterService{}}

	yes := doRequest(withStartStop.Handler(), http.MethodGet, "/api/v1/config", "")
	no := doRequest(without.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Contains(t, yes.Body.String(), `"clusterStartStop":true`)
	assert.Contains(t, no.Body.String(), `"clusterStartStop":false`)
}

// TestStartStopRoutesReachBackend covers the additive start/stop endpoints: a backend implementing
// ClusterLifecycleController has the routes registered and they invoke Start/Stop, returning 202.
func TestStartStopRoutesReachBackend(t *testing.T) {
	t.Parallel()

	backend := &lifecycleStub{}
	server := &api.Server{Service: backend}
	handler := server.Handler()

	start := doRequest(handler, http.MethodPost, "/api/v1/clusters/default/c1/start", "")
	assert.Equal(t, http.StatusAccepted, start.Code)

	stop := doRequest(handler, http.MethodPost, "/api/v1/clusters/default/c1/stop", "")
	assert.Equal(t, http.StatusAccepted, stop.Code)

	assert.Equal(t, []string{"c1"}, backend.started)
	assert.Equal(t, []string{"c1"}, backend.stopped)
}

// TestStartStopRoutesUnregisteredWithoutCapability pins that a backend without
// ClusterLifecycleController does not expose the start/stop routes: the SPA catch-all (or, here, a 404
// from the mux) handles them rather than a misleading success.
func TestStartStopRoutesUnregisteredWithoutCapability(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: stubClusterService{}}
	handler := server.Handler()

	start := doRequest(handler, http.MethodPost, "/api/v1/clusters/default/c1/start", "")
	assert.Equal(t, http.StatusNotFound, start.Code)
}

// TestStartStopRejectedInReadOnly pins that the start/stop endpoints (POST verbs) are blocked by the
// read-only guard, like the other mutating routes.
func TestStartStopRejectedInReadOnly(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: &lifecycleStub{}, ReadOnly: true}
	handler := server.Handler()

	start := doRequest(handler, http.MethodPost, "/api/v1/clusters/default/c1/start", "")
	assert.Equal(t, http.StatusForbidden, start.Code)
}

func TestConfigReportsMode(t *testing.T) {
	t.Parallel()

	// The serving surface (operator vs. local `ksail open web`) is reported so the SPA can label the UI
	// accurately; it is omitted when unset.
	server := &api.Server{
		Service: operator.NewCRClusterService(newClient(t)),
		Mode:    api.ModeOperator,
	}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"mode":"operator"`)
}

func TestStaticFSServesAssetsAndSPAFallback(t *testing.T) {
	t.Parallel()

	staticFS := fstest.MapFS{
		"index.html":    {Data: []byte("<html>spa</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	server := &api.Server{
		Service:  operator.NewCRClusterService(newClient(t, sampleCluster())),
		StaticFS: staticFS,
	}

	handler := server.Handler()

	// Root serves index.html.
	root := doRequest(handler, http.MethodGet, "/", "")
	assert.Equal(t, http.StatusOK, root.Code)
	assert.Contains(t, root.Body.String(), "<html>spa</html>")

	// A real asset is served from the embedded FS.
	asset := doRequest(handler, http.MethodGet, "/assets/app.js", "")
	assert.Equal(t, http.StatusOK, asset.Code)
	assert.Contains(t, asset.Body.String(), "console.log(1)")

	// An unknown client-side route falls back to index.html (SPA routing).
	spa := doRequest(handler, http.MethodGet, "/clusters/some/route", "")
	assert.Equal(t, http.StatusOK, spa.Code)
	assert.Contains(t, spa.Body.String(), "<html>spa</html>")

	// API routes still take precedence over the static catch-all.
	apiResp := doRequest(handler, http.MethodGet, "/api/v1/clusters", "")
	assert.Equal(t, http.StatusOK, apiResp.Code)
	assert.Contains(t, apiResp.Body.String(), "c1")
}

func TestConfigIncludesDistributions(t *testing.T) {
	t.Parallel()

	server := &api.Server{
		Service:       operator.NewCRClusterService(newClient(t)),
		Distributions: []string{"Vanilla", "K3s", "VCluster"},
	}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"distributions":["Vanilla","K3s","VCluster"]`)
}

func TestConfigOmitsProvidersWhenStatusUnset(t *testing.T) {
	t.Parallel()

	// The operator leaves ProviderStatus nil; the SPA then offers every provider (no gating).
	server := &api.Server{Service: operator.NewCRClusterService(newClient(t))}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), `"providers"`)
}

func TestConfigIncludesProviderStatusWhenSet(t *testing.T) {
	t.Parallel()

	server := &api.Server{
		Service: operator.NewCRClusterService(newClient(t)),
		ProviderStatus: func(context.Context) []api.ProviderInfo {
			return []api.ProviderInfo{
				{Name: "Docker", Available: true},
				{Name: "Hetzner", Available: false, Reason: "HCLOUD_TOKEN is not set"},
			}
		},
	}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, `"providers"`)
	assert.Contains(t, body, `"name":"Docker","available":true`)
	assert.Contains(
		t,
		body,
		`"name":"Hetzner","available":false,"reason":"HCLOUD_TOKEN is not set"`,
	)
}

const sessionSecret = "0123456789abcdef0123456789abcdef"

func TestAuthGuardRejectsUnauthenticated(t *testing.T) {
	t.Parallel()

	server := api.NewAuthTestServer(
		operator.NewCRClusterService(newClient(t, sampleCluster())),
		[]byte(sessionSecret),
	)

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/clusters", "")

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "loginURL")
}

func TestAuthGuardAllowsValidSession(t *testing.T) {
	t.Parallel()

	server := api.NewAuthTestServer(
		operator.NewCRClusterService(newClient(t, sampleCluster())),
		[]byte(sessionSecret),
	)

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/clusters",
		nil,
	)
	request.AddCookie(server.NewSessionCookie("alice@example.com"))

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestReadOnlyAllowsAuthLogout(t *testing.T) {
	t.Parallel()

	server := api.NewAuthTestServer(
		operator.NewCRClusterService(newClient(t)),
		[]byte(sessionSecret),
	)
	server.ReadOnly = true

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/auth/logout",
		nil,
	)
	request.AddCookie(server.NewSessionCookie("alice@example.com"))

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	// Read-only constrains cluster mutations, not session management, so logout must still work.
	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestAuthGuardLeavesConfigOpen(t *testing.T) {
	t.Parallel()

	server := api.NewAuthTestServer(
		operator.NewCRClusterService(newClient(t)),
		[]byte(sessionSecret),
	)

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"authEnabled":true`)
}
