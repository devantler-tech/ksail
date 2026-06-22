package api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// watchStub implements api.KubeWatch, returning a canned newline-delimited watch JSON stream so the SSE
// framing can be tested without a real apiserver. capturedQuery records what the handler forwarded so a
// test can assert watch=true is propagated.
type watchStub struct {
	stubClusterService

	content       string
	capturedPath  string
	capturedQuery url.Values
	// block, when non-nil, is returned instead of a canned reader so a test can drive a stream that
	// never ends (to assert the handler stops on context cancellation).
	block io.ReadCloser
}

func (w *watchStub) WatchKube(
	_ context.Context, _, _, apiPath string, query url.Values,
) (io.ReadCloser, error) {
	w.capturedPath = apiPath
	w.capturedQuery = query

	if w.block != nil {
		return w.block, nil
	}

	return io.NopCloser(strings.NewReader(w.content)), nil
}

func TestConfigReportsKubeWatch(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: &watchStub{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"kubeWatch":true`)
}

func TestKubeWatchUnregisteredWithoutKubeWatch(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: stubClusterService{}}

	config := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")
	assert.Contains(t, config.Body.String(), `"kubeWatch":false`)

	watch := doRequest(
		server.Handler(),
		http.MethodGet,
		"/api/v1/clusters/default/c1/watch/api/v1/pods",
		"",
	)
	assert.Equal(t, http.StatusNotFound, watch.Code)
}

func TestKubeWatchSSEStream(t *testing.T) {
	t.Parallel()

	// Two watch events as the apiserver would stream them: newline-delimited JSON objects.
	stub := &watchStub{
		content: `{"type":"ADDED","object":{"metadata":{"uid":"u1"}}}` + "\n" +
			`{"type":"DELETED","object":{"metadata":{"uid":"u1"}}}` + "\n",
	}
	server := &api.Server{Service: stub}

	recorder := doRequest(
		server.Handler(),
		http.MethodGet,
		"/api/v1/clusters/default/c1/watch/api/v1/pods",
		"",
	)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))

	body := recorder.Body.String()
	assert.Contains(t, body, `event: watch`)
	assert.Contains(t, body, `data: {"type":"ADDED","object":{"metadata":{"uid":"u1"}}}`)
	assert.Contains(t, body, `data: {"type":"DELETED","object":{"metadata":{"uid":"u1"}}}`)
	assert.Contains(t, body, "event: eof") // stream end signalled once the apiserver stream closes

	// The handler forwards the apiserver path verbatim (watch=true is forced in the client, not here).
	assert.Equal(t, "api/v1/pods", stub.capturedPath)
}

func TestKubeWatchAllowedWhenReadOnly(t *testing.T) {
	t.Parallel()

	// A watch is read-only, so the endpoint streams even in read-only mode (unlike the write actions).
	stub := &watchStub{content: `{"type":"ADDED","object":{}}` + "\n"}
	server := &api.Server{Service: stub, ReadOnly: true}

	recorder := doRequest(
		server.Handler(),
		http.MethodGet,
		"/api/v1/clusters/default/c1/watch/api/v1/pods",
		"",
	)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

// blockingReader blocks on Read until its context is cancelled, then returns io.EOF. It models an
// apiserver watch that stays open with no events, so the test can assert the handler stops streaming as
// soon as the client disconnects (request context cancelled) rather than hanging.
type blockingReader struct {
	ctx context.Context //nolint:containedctx // models a stream whose lifetime follows the request ctx.
}

func (b blockingReader) Read([]byte) (int, error) {
	<-b.ctx.Done()

	return 0, io.EOF
}

func (b blockingReader) Close() error { return nil }

func TestKubeWatchStopsOnClientDisconnect(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	stub := &watchStub{block: blockingReader{ctx: ctx}}
	server := &api.Server{Service: stub}

	request := httptest.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"/api/v1/clusters/default/c1/watch/api/v1/pods",
		nil,
	)
	recorder := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)

		server.Handler().ServeHTTP(recorder, request)
	}()

	// Cancel the request context (the client disconnecting); the handler must return rather than block
	// forever on the never-ending stream.
	cancel()

	select {
	case <-done:
		// handler returned promptly after cancellation, as required.
	case <-time.After(5 * time.Second):
		require.FailNow(t, "handleKubeWatch did not stop after the client disconnected")
	}
}
