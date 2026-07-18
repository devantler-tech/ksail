package api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// execStub implements api.ExecService by echoing stdin back to stdout, so the WebSocket bridge can be
// tested end-to-end without a real cluster.
type execStub struct {
	stubClusterService
}

func (execStub) Exec(
	_ context.Context, _, _ string, _ api.ExecRequest, streams api.ExecStreams,
) error {
	go func() {
		for range streams.Resize { //nolint:revive // drain so the resize queue never blocks
		}
	}()

	_, _ = io.Copy(streams.Stdout, streams.Stdin)

	return nil
}

func execWebSocketURL(httpURL, path string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + path
}

func TestConfigReportsWorkloadExec(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: execStub{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"workloadExec":true`)
}

func TestExecWebSocketBridge(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer((&api.Server{Service: execStub{}}).Handler())
	defer server.Close()

	url := execWebSocketURL(server.URL, "/api/v1/clusters/default/c1/exec?pod=p1")

	header := http.Header{}
	header.Set("Origin", server.URL)

	conn, response, err := websocket.DefaultDialer.Dial(url, header)
	require.NoError(t, err)

	if response != nil {
		defer func() { _ = response.Body.Close() }()
	}

	defer func() { _ = conn.Close() }()

	require.NoError(t, conn.WriteJSON(map[string]any{"op": "stdin", "data": "hello"}))

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var msg struct {
		Op   string `json:"op"`
		Data string `json:"data"`
	}

	require.NoError(t, conn.ReadJSON(&msg))
	assert.Equal(t, "stdout", msg.Op)
	assert.Equal(t, "hello", msg.Data)
}

func TestExecRejectsCrossOriginWebSocket(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer((&api.Server{Service: execStub{}}).Handler())
	defer server.Close()

	url := execWebSocketURL(server.URL, "/api/v1/clusters/default/c1/exec?pod=p1")
	header := http.Header{}
	header.Set("Origin", "https://attacker.example")

	_, response, err := websocket.DefaultDialer.Dial(url, header)
	require.Error(t, err)
	require.NotNil(t, response)
	defer func() { _ = response.Body.Close() }()

	assert.Equal(t, http.StatusForbidden, response.StatusCode)
}

func TestExecBlockedWhenReadOnly(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer((&api.Server{Service: execStub{}, ReadOnly: true}).Handler())
	defer server.Close()

	url := execWebSocketURL(server.URL, "/api/v1/clusters/default/c1/exec?pod=p1")

	_, response, err := websocket.DefaultDialer.Dial(url, nil)
	require.Error(t, err) // upgrade refused before switching protocols

	require.NotNil(t, response)

	defer func() { _ = response.Body.Close() }()

	assert.Equal(t, http.StatusForbidden, response.StatusCode)

	// The body must match the readOnlyGuard's byte-for-byte: the SPA parses one shape.
	body, readErr := io.ReadAll(response.Body)
	require.NoError(t, readErr)
	//nolint:testifylint // assert the exact bytes: the body is a wire contract, JSON-equivalence is too weak
	assert.Equal(t, readOnlyBody, string(body))
}
