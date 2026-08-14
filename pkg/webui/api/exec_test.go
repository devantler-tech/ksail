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

// execPath is the exec endpoint every exec test drives; it is fixed so the tests differ only in the
// headers they present.
const execPath = "/api/v1/clusters/default/c1/exec?pod=p1"

func execWebSocketURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + execPath
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

	url := execWebSocketURL(server.URL)

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

	url := execWebSocketURL(server.URL)
	header := http.Header{}
	header.Set("Origin", "https://attacker.example")

	_, response, err := websocket.DefaultDialer.Dial(url, header)
	require.Error(t, err)
	require.NotNil(t, response)

	defer func() { _ = response.Body.Close() }()

	assert.Equal(t, http.StatusForbidden, response.StatusCode)
}

// TestExecRejectsRebindingOriginMatchingHost pins the DNS-rebinding case, which a same-origin policy
// cannot catch: the attacker's hostname has been rebound to loopback, so Origin and Host agree and
// gorilla's default CheckOrigin would accept the upgrade. Only anchoring on the loopback identity
// rejects it.
func TestExecRejectsRebindingOriginMatchingHost(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer((&api.Server{Service: execStub{}}).Handler())
	defer server.Close()

	url := execWebSocketURL(server.URL)
	header := http.Header{}
	// gorilla's Dialer promotes a "Host" header into the request's Host field, so this reproduces a
	// rebound name resolving to the loopback listener with a self-consistent Origin/Host pair.
	header.Set("Host", "attacker.example")
	header.Set("Origin", "http://attacker.example")

	_, response, err := websocket.DefaultDialer.Dial(url, header)
	require.Error(t, err)
	require.NotNil(t, response)

	defer func() { _ = response.Body.Close() }()

	assert.Equal(t, http.StatusForbidden, response.StatusCode)
}

// TestExecAllowsLocalhostOrigin pins the everyday path: the SPA served from the loopback listener under
// the "localhost" spelling still upgrades, so the hardening costs the operator nothing.
func TestExecAllowsLocalhostOrigin(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer((&api.Server{Service: execStub{}}).Handler())
	defer server.Close()

	url := execWebSocketURL(server.URL)
	header := http.Header{}
	header.Set("Origin", "http://localhost:8080")

	conn, response, err := websocket.DefaultDialer.Dial(url, header)
	require.NoError(t, err)

	if response != nil {
		defer func() { _ = response.Body.Close() }()
	}

	defer func() { _ = conn.Close() }()
}

func TestExecBlockedWhenReadOnly(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer((&api.Server{Service: execStub{}, ReadOnly: true}).Handler())
	defer server.Close()

	url := execWebSocketURL(server.URL)

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
