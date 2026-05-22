package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const defaultNS = "default"

func newClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
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
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	return recorder
}

func TestConfigReportsReadOnly(t *testing.T) {
	t.Parallel()

	server := &api.Server{Client: newClient(t), ReadOnly: true}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"readOnly":true}`, recorder.Body.String())
}

func TestListClusters(t *testing.T) {
	t.Parallel()

	server := &api.Server{Client: newClient(t, sampleCluster())}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/clusters", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "c1")
}

func TestGetClusterNotFound(t *testing.T) {
	t.Parallel()

	server := &api.Server{Client: newClient(t)}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/clusters/default/missing", "")

	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestCreateClusterWhenWritable(t *testing.T) {
	t.Parallel()

	server := &api.Server{Client: newClient(t)}
	body := `{"metadata":{"name":"new","namespace":"default"},"spec":{"cluster":{"distribution":"VCluster"}}}`

	recorder := doRequest(server.Handler(), http.MethodPost, "/api/v1/clusters", body)

	assert.Equal(t, http.StatusCreated, recorder.Code)
}

func TestReadOnlyRejectsCreate(t *testing.T) {
	t.Parallel()

	server := &api.Server{Client: newClient(t), ReadOnly: true}
	body := `{"metadata":{"name":"new","namespace":"default"}}`

	recorder := doRequest(server.Handler(), http.MethodPost, "/api/v1/clusters", body)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "readOnly")
}

func TestReadOnlyRejectsDelete(t *testing.T) {
	t.Parallel()

	server := &api.Server{Client: newClient(t, sampleCluster()), ReadOnly: true}

	recorder := doRequest(server.Handler(), http.MethodDelete, "/api/v1/clusters/default/c1", "")

	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestReadOnlyAllowsReads(t *testing.T) {
	t.Parallel()

	server := &api.Server{Client: newClient(t, sampleCluster()), ReadOnly: true}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/clusters", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestDeleteClusterWhenWritable(t *testing.T) {
	t.Parallel()

	fakeClient := newClient(t, sampleCluster())
	server := &api.Server{Client: fakeClient}

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

	server := &api.Server{Client: newClient(t)}

	recorder := doRequest(server.Handler(), http.MethodGet, "/healthz", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestUpdateClusterAppliesSpec(t *testing.T) {
	t.Parallel()

	fakeClient := newClient(t, sampleCluster())
	server := &api.Server{Client: fakeClient}
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

	server := &api.Server{Client: newClient(t)}
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
	server := &api.Server{Client: fakeClient}
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
	server := &api.Server{Client: fakeClient}
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

	server := &api.Server{Client: newClient(t, sampleCluster())}
	body := `{"metadata":{"name":"c1","namespace":"default"},"spec":{"cluster":{"distribution":"VCluster"}}}`

	recorder := doRequest(server.Handler(), http.MethodPost, "/api/v1/clusters", body)
	assert.Equal(t, http.StatusConflict, recorder.Code)
}

func TestConfigDefaultsWritable(t *testing.T) {
	t.Parallel()

	server := &api.Server{Client: newClient(t)}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"readOnly":false}`, recorder.Body.String())
}
