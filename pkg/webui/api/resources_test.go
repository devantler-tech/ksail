package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
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

// writerStub additionally implements api.ResourceWriter, recording the action it received.
type writerStub struct {
	resourceStub

	scaledTo   int32
	restarted  bool
	deleted    bool
	reconciled bool
}

func (w *writerStub) ScaleResource(
	_ context.Context, _, _ string, _ api.ResourceRef, replicas int32,
) error {
	w.scaledTo = replicas

	return nil
}

func (w *writerStub) RestartResource(_ context.Context, _, _ string, _ api.ResourceRef) error {
	w.restarted = true

	return nil
}

func (w *writerStub) DeleteResource(_ context.Context, _, _ string, _ api.ResourceRef) error {
	w.deleted = true

	return nil
}

func (w *writerStub) ReconcileResource(_ context.Context, _, _ string, _ api.ResourceRef) error {
	w.reconciled = true

	return nil
}

func TestConfigReportsWorkloadWriteForResourceWriter(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: &writerStub{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"workloadWrite":true`)
}

func TestScaleResourceEndpoint(t *testing.T) {
	t.Parallel()

	stub := &writerStub{}
	server := &api.Server{Service: stub}

	recorder := doRequest(
		server.Handler(),
		http.MethodPut,
		"/api/v1/clusters/default/c1/resources/Deployment/web/scale?namespace=x",
		`{"replicas":3}`,
	)

	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, int32(3), stub.scaledTo)
}

func TestRestartResourceEndpoint(t *testing.T) {
	t.Parallel()

	stub := &writerStub{}
	server := &api.Server{Service: stub}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/clusters/default/c1/resources/Deployment/web/restart?namespace=x",
		"",
	)

	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.True(t, stub.restarted)
}

func TestReconcileResourceEndpoint(t *testing.T) {
	t.Parallel()

	stub := &writerStub{}
	server := &api.Server{Service: stub}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/clusters/default/c1/resources/Kustomization/apps/reconcile?namespace=flux-system",
		"",
	)

	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.True(t, stub.reconciled)
}

func TestDeleteResourceEndpoint(t *testing.T) {
	t.Parallel()

	stub := &writerStub{}
	server := &api.Server{Service: stub}

	recorder := doRequest(
		server.Handler(),
		http.MethodDelete,
		"/api/v1/clusters/default/c1/resources/Pod/p1?namespace=x",
		"",
	)

	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.True(t, stub.deleted)
}

func TestWriteEndpointsBlockedWhenReadOnly(t *testing.T) {
	t.Parallel()

	stub := &writerStub{}
	server := &api.Server{Service: stub, ReadOnly: true}

	recorder := doRequest(
		server.Handler(),
		http.MethodPut,
		"/api/v1/clusters/default/c1/resources/Deployment/web/scale?namespace=x",
		`{"replicas":3}`,
	)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Equal(t, int32(0), stub.scaledTo)
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

func TestResourceKindForGitOps(t *testing.T) {
	t.Parallel()

	// GitOps CRs resolve at their served (non-v1) versions.
	helm, err := api.ResourceKindFor("HelmRelease")
	require.NoError(t, err)
	assert.Equal(t, "helm.toolkit.fluxcd.io", helm.GVR.Group)
	assert.Equal(t, "v2", helm.GVR.Version)
	assert.Equal(t, "helmreleases", helm.GVR.Resource)
	assert.True(t, helm.Namespaced)

	kustomization, err := api.ResourceKindFor("Kustomization")
	require.NoError(t, err)
	assert.Equal(t, "kustomize.toolkit.fluxcd.io", kustomization.GVR.Group)
	assert.Equal(t, "v1", kustomization.GVR.Version)

	app, err := api.ResourceKindFor("Application")
	require.NoError(t, err)
	assert.Equal(t, "argoproj.io", app.GVR.Group)
	assert.Equal(t, "v1alpha1", app.GVR.Version)
}

func TestResourceKindForMetrics(t *testing.T) {
	t.Parallel()

	// Metrics kinds resolve at metrics.k8s.io/v1beta1 (the served version); NodeMetrics is
	// cluster-scoped and PodMetrics is namespaced, mirroring their Node/Pod counterparts.
	nodeMetrics, err := api.ResourceKindFor("NodeMetrics")
	require.NoError(t, err)
	assert.Equal(t, "metrics.k8s.io", nodeMetrics.GVR.Group)
	assert.Equal(t, "v1beta1", nodeMetrics.GVR.Version)
	assert.Equal(t, "nodes", nodeMetrics.GVR.Resource)
	assert.False(t, nodeMetrics.Namespaced)

	podMetrics, err := api.ResourceKindFor("PodMetrics")
	require.NoError(t, err)
	assert.Equal(t, "metrics.k8s.io", podMetrics.GVR.Group)
	assert.Equal(t, "v1beta1", podMetrics.GVR.Version)
	assert.Equal(t, "pods", podMetrics.GVR.Resource)
	assert.True(t, podMetrics.Namespaced)
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
