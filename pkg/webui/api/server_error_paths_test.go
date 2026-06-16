package api_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// errBackend is a generic (non-sentinel) backend failure, which clientErrorStatus maps to 500. It
// exercises the handlers' "the backend returned an error" branch, distinct from the sentinel-mapped
// statuses already asserted by the happy-path and not-found tests.
var errBackend = errors.New("backend boom")

// errClusterService injects List/Delete failures so the cluster handlers' error branches — which the
// always-succeeding stubClusterService never reaches — are exercised through the real HTTP handler.
type errClusterService struct {
	stubClusterService

	listErr   error
	deleteErr error
}

func (s errClusterService) List(context.Context) (*v1alpha1.ClusterList, error) {
	return nil, s.listErr
}

func (s errClusterService) Delete(context.Context, string, string) error {
	return s.deleteErr
}

// errResourceStub injects ListResources/GetResource failures so the resource read handlers' error
// branches are exercised. It embeds resourceStub, so it satisfies ResourceService.
type errResourceStub struct {
	resourceStub

	err error
}

func (r errResourceStub) ListResources(
	context.Context, string, string, api.ResourceQuery,
) (*unstructured.UnstructuredList, error) {
	return nil, r.err
}

func (r errResourceStub) GetResource(
	context.Context, string, string, api.ResourceRef,
) (*unstructured.Unstructured, error) {
	return nil, r.err
}

// errWriterStub injects ResourceWriter failures so the write handlers' error branches (handleScale
// directly, restart/delete via runResourceWrite) are exercised. It embeds resourceStub so the
// resource routes register, and adds the write methods to satisfy ResourceWriter.
type errWriterStub struct {
	resourceStub

	err error
}

func (w errWriterStub) ScaleResource(
	context.Context, string, string, api.ResourceRef, int32,
) error {
	return w.err
}

func (w errWriterStub) RestartResource(context.Context, string, string, api.ResourceRef) error {
	return w.err
}

func (w errWriterStub) DeleteResource(context.Context, string, string, api.ResourceRef) error {
	return w.err
}

func (w errWriterStub) ReconcileResource(context.Context, string, string, api.ResourceRef) error {
	return w.err
}

func TestListClustersBackendErrorReturns500(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: errClusterService{listErr: errBackend}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/clusters", "")

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), errBackend.Error())
}

func TestDeleteClusterNotFoundReturns404(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: errClusterService{deleteErr: api.ErrNotFound}}

	recorder := doRequest(
		server.Handler(),
		http.MethodDelete,
		"/api/v1/clusters/default/missing",
		"",
	)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestListResourcesBackendErrorReturns500(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: errResourceStub{err: errBackend}}

	recorder := doRequest(
		server.Handler(),
		http.MethodGet,
		"/api/v1/clusters/default/c1/resources?kind=Pod",
		"",
	)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), errBackend.Error())
}

func TestGetResourceNotFoundReturns404(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: errResourceStub{err: api.ErrNotFound}}

	recorder := doRequest(
		server.Handler(),
		http.MethodGet,
		"/api/v1/clusters/default/c1/resources/Pod/p1?namespace=x",
		"",
	)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestScaleResourceBackendErrorMapsStatus(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: errWriterStub{err: api.ErrNotFound}}

	recorder := doRequest(
		server.Handler(),
		http.MethodPut,
		"/api/v1/clusters/default/c1/resources/Deployment/web/scale?namespace=x",
		`{"replicas":3}`,
	)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestScaleResourceMalformedBodyReturns400(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: &writerStub{}}

	recorder := doRequest(
		server.Handler(),
		http.MethodPut,
		"/api/v1/clusters/default/c1/resources/Deployment/web/scale?namespace=x",
		"{not json",
	)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestRestartResourceBackendErrorMapsStatus(t *testing.T) {
	t.Parallel()

	// Restart routes through runResourceWrite, so this also covers its service-error branch.
	server := &api.Server{Service: errWriterStub{err: errBackend}}

	recorder := doRequest(
		server.Handler(),
		http.MethodPost,
		"/api/v1/clusters/default/c1/resources/Deployment/web/restart?namespace=x",
		"",
	)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), errBackend.Error())
}
