package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const metaNameKey = "name"

// resourceStub is a ClusterService that also implements api.ResourceService, returning canned
// objects so the HTTP layer (routing, params, capability, JSON) can be tested without a cluster.
type resourceStub struct {
	stubClusterService

	list *unstructured.UnstructuredList
	obj  *unstructured.Unstructured
}

func (r resourceStub) ListResources(
	_ context.Context,
	_, _ string,
	_ api.ResourceQuery,
) (*unstructured.UnstructuredList, error) {
	return r.list, nil
}

func (r resourceStub) GetResource(
	_ context.Context,
	_, _ string,
	_ api.ResourceRef,
) (*unstructured.Unstructured, error) {
	return r.obj, nil
}

func TestResourceKindForAllowlist(t *testing.T) {
	t.Parallel()

	pod, err := api.ResourceKindFor("Pod")
	require.NoError(t, err)
	assert.Equal(t, "pods", pod.GVR.Resource)
	assert.True(t, pod.Namespaced)

	node, err := api.ResourceKindFor("Node")
	require.NoError(t, err)
	assert.False(t, node.Namespaced)

	// Secrets are intentionally excluded (sensitive values); unknown kinds are rejected as invalid.
	_, err = api.ResourceKindFor("Secret")
	require.ErrorIs(t, err, api.ErrInvalid)
}

func TestConfigReportsWorkloadReadForResourceService(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: resourceStub{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"workloadRead":true`)
}

func TestListResourcesEndpoint(t *testing.T) {
	t.Parallel()

	list := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
		{Object: map[string]any{"kind": "Pod", "metadata": map[string]any{metaNameKey: "p1"}}},
	}}
	server := &api.Server{Service: resourceStub{list: list}}

	recorder := doRequest(
		server.Handler(),
		http.MethodGet,
		"/api/v1/clusters/default/c1/resources?kind=Pod",
		"",
	)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "p1")
}

func TestGetResourceEndpoint(t *testing.T) {
	t.Parallel()

	obj := &unstructured.Unstructured{Object: map[string]any{
		"kind": "Pod", "metadata": map[string]any{"name": "p1", "namespace": "x"},
	}}
	server := &api.Server{Service: resourceStub{obj: obj}}

	recorder := doRequest(
		server.Handler(),
		http.MethodGet,
		"/api/v1/clusters/default/c1/resources/Pod/p1?namespace=x",
		"",
	)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "p1")
}

func TestResourceEndpointsUnregisteredWithoutResourceService(t *testing.T) {
	t.Parallel()

	// A plain ClusterService (no ResourceService) does not get the resource routes, and config
	// reports workloadRead:false so the SPA hides the view.
	server := &api.Server{Service: stubClusterService{}}

	config := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")
	assert.Contains(t, config.Body.String(), `"workloadRead":false`)

	// The /resources path has an extra segment beyond GET /clusters/{ns}/{name}, so without the route
	// (and without StaticFS) the mux does not match it.
	resources := doRequest(
		server.Handler(),
		http.MethodGet,
		"/api/v1/clusters/default/c1/resources?kind=Pod",
		"",
	)
	assert.Equal(t, http.StatusNotFound, resources.Code)
}
