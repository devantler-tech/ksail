package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCreateClusterCreatesMissingNamespace(t *testing.T) {
	t.Parallel()

	kubeClient := newClient(t)
	server := &api.Server{Service: api.NewCRClusterService(kubeClient)}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/clusters",
		`{"metadata":{"name":"c1","namespace":"newns"},"spec":{"cluster":{"distribution":"VCluster"}}}`,
	)
	require.Equal(t, http.StatusCreated, recorder.Code)

	var namespace corev1.Namespace

	require.NoError(
		t,
		kubeClient.Get(context.Background(), client.ObjectKey{Name: "newns"}, &namespace),
	)
	assert.Equal(t, "true", namespace.Labels[v1alpha1.ManagedNamespaceLabel])

	var cluster v1alpha1.Cluster

	require.NoError(
		t,
		kubeClient.Get(
			context.Background(),
			client.ObjectKey{Name: "c1", Namespace: "newns"},
			&cluster,
		),
	)
}

func TestCreateClusterPreservesExistingNamespace(t *testing.T) {
	t.Parallel()

	existing := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "preexisting"}}
	kubeClient := newClient(t, existing)
	server := &api.Server{Service: api.NewCRClusterService(kubeClient)}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/clusters",
		`{"metadata":{"name":"c1","namespace":"preexisting"},"spec":{"cluster":{"distribution":"VCluster"}}}`,
	)
	require.Equal(t, http.StatusCreated, recorder.Code)

	var namespace corev1.Namespace

	require.NoError(
		t,
		kubeClient.Get(context.Background(), client.ObjectKey{Name: "preexisting"}, &namespace),
	)
	assert.NotEqual(
		t,
		"true",
		namespace.Labels[v1alpha1.ManagedNamespaceLabel],
		"a pre-existing namespace must not be labelled operator-managed",
	)
}
