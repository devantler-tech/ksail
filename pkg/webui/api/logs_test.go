package api_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
)

// logStub implements api.LogService, returning a canned log stream so the SSE framing can be tested
// without a real cluster.
type logStub struct {
	stubClusterService

	content string
}

func (l logStub) PodLogs(
	_ context.Context, _, _ string, _ api.LogRequest,
) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(l.content)), nil
}

func TestConfigReportsWorkloadLogs(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: logStub{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"workloadLogs":true`)
}

func TestLogsSSEStream(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: logStub{content: "line one\nline two\n"}}

	recorder := doRequest(
		server.Handler(),
		http.MethodGet,
		"/api/v1/clusters/default/c1/logs?pod=p1&container=c",
		"",
	)

	assert.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, "event: log\ndata: line one")
	assert.Contains(t, body, "event: log\ndata: line two")
	assert.Contains(t, body, "event: eof") // stream end signalled
}

func TestLogsRequiresPod(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: logStub{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/clusters/default/c1/logs", "")

	assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
}

func TestLogsAllowedWhenReadOnly(t *testing.T) {
	t.Parallel()

	// Logs are read-only, so the endpoint streams even in read-only mode (unlike the write actions).
	server := &api.Server{Service: logStub{content: "x\n"}, ReadOnly: true}

	recorder := doRequest(
		server.Handler(),
		http.MethodGet,
		"/api/v1/clusters/default/c1/logs?pod=p1",
		"",
	)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestLogsUnregisteredWithoutLogService(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: stubClusterService{}}

	config := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")
	assert.Contains(t, config.Body.String(), `"workloadLogs":false`)

	logs := doRequest(
		server.Handler(),
		http.MethodGet,
		"/api/v1/clusters/default/c1/logs?pod=p1",
		"",
	)
	assert.Equal(t, http.StatusNotFound, logs.Code)
}
