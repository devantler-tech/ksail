package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wsMuxMessage mirrors the multiplexer wire frame the handler sends and receives, so the test can assert
// the exact Headlamp field names (clusterId, path, query, data, type) are produced on the wire.
type wsMuxMessage struct {
	ClusterID string `json:"clusterId"`
	Path      string `json:"path"`
	Query     string `json:"query"`
	UserID    string `json:"userId"`
	Data      string `json:"data,omitempty"`
	Type      string `json:"type"`
}

// pipeWatchStub implements api.KubeWatch by handing back the read end of an io.Pipe, so a test drives the
// watch stream byte by byte (emitting events on demand) and observes teardown: a watch's Close is
// recorded, and a second pipe lets a later request open a fresh watch. The captured path/query (what the
// handler forwarded) are guarded by a mutex because WatchKube runs on the server goroutine while the test
// asserts on them — accessor methods keep the access race-free under -race.
type pipeWatchStub struct {
	stubClusterService

	reader io.ReadCloser
	closed *atomic.Bool

	mu            sync.Mutex
	capturedPath  string
	capturedQuery url.Values
}

func (p *pipeWatchStub) WatchKube(
	ctx context.Context, _, _, apiPath string, query url.Values,
) (io.ReadCloser, error) {
	p.mu.Lock()
	p.capturedPath = apiPath
	p.capturedQuery = query
	p.mu.Unlock()

	// Tie the returned reader's Close to ctx cancellation as well, so a CLOSE/disconnect (which cancels
	// the subscription ctx) unblocks a reader parked in Read, modelling the apiserver hanging up.
	go func() {
		<-ctx.Done()

		_ = p.reader.Close()
	}()

	return p.reader, nil
}

// path returns the apiserver path WatchKube last captured, under the mutex.
func (p *pipeWatchStub) path() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.capturedPath
}

// query returns the url.Values WatchKube last captured, under the mutex.
func (p *pipeWatchStub) query() url.Values {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.capturedQuery
}

// dialWSMux opens a WebSocket to the multiplexer endpoint of a test server.
func dialWSMux(t *testing.T, serverURL string) (*websocket.Conn, func()) {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/wsMultiplexer"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, response, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)

	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}

	return conn, func() { _ = conn.Close(websocket.StatusNormalClosure, "") }
}

// readWSMux reads one frame from the connection and decodes it into a wsMuxMessage.
func readWSMux(t *testing.T, conn *websocket.Conn) wsMuxMessage {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	typ, data, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, websocket.MessageText, typ)

	var msg wsMuxMessage
	require.NoError(t, json.Unmarshal(data, &msg))

	return msg
}

// sendWSMux marshals and sends a control frame.
func sendWSMux(t *testing.T, conn *websocket.Conn, msg wsMuxMessage) {
	t.Helper()

	encoded, err := json.Marshal(msg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, conn.Write(ctx, websocket.MessageText, encoded))
}

func TestConfigReportsWSMultiplexer(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: &watchStub{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"wsMultiplexer":true`)
}

func TestWSMultiplexerUnregisteredWithoutKubeWatch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer((&api.Server{Service: stubClusterService{}}).Handler())
	defer server.Close()

	config := doRequest(
		(&api.Server{Service: stubClusterService{}}).Handler(),
		http.MethodGet,
		"/api/v1/config",
		"",
	)
	assert.Contains(t, config.Body.String(), `"wsMultiplexer":false`)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/wsMultiplexer"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, response, err := websocket.Dial(ctx, wsURL, nil)
	if conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}

	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	// No KubeWatch backing → the route is not registered → the upgrade fails (no WebSocket handshake).
	require.Error(t, err)
}

// TestWSMultiplexerStreamsAndTearsDown drives the full Headlamp wire protocol: a client REQUESTs a path,
// the fake apiserver emits a watch event, the handler frames it as a DATA message keyed by
// {clusterId, path, query}; a CLOSE then tears the watch down (the upstream reader is closed).
func TestWSMultiplexerStreamsAndTearsDown(t *testing.T) {
	t.Parallel()

	reader, writer := io.Pipe()
	closed := &atomic.Bool{}
	stub := &pipeWatchStub{
		reader: &closeTrackingReader{ReadCloser: reader, closed: closed},
		closed: closed,
	}

	server := httptest.NewServer((&api.Server{Service: stub}).Handler())
	defer server.Close()

	conn, cleanup := dialWSMux(t, server.URL)
	defer cleanup()

	const (
		cluster = "c1"
		path    = "/api/v1/pods"
		query   = "labelSelector=app%3Dnginx"
	)

	sendWSMux(
		t,
		conn,
		wsMuxMessage{ClusterID: cluster, Path: path, Query: query, UserID: "u1", Type: "REQUEST"},
	)

	// The apiserver emits one watch event line; the handler must deliver it as a DATA frame.
	event := `{"type":"ADDED","object":{"metadata":{"uid":"u1"}}}`

	go func() {
		// Give the subscription a moment to open before writing, then emit the event.
		time.Sleep(50 * time.Millisecond)

		_, _ = writer.Write([]byte(event + "\n"))
	}()

	msg := readWSMux(t, conn)
	assert.Equal(t, "DATA", msg.Type, "watch events are framed as Headlamp DATA messages")
	assert.Equal(t, cluster, msg.ClusterID, "DATA echoes clusterId so the client re-keys it")
	assert.Equal(t, path, msg.Path, "DATA echoes path")
	assert.Equal(t, query, msg.Query, "DATA echoes query (part of the subscription key)")
	assert.JSONEq(
		t,
		event,
		msg.Data,
		"DATA.data carries the raw apiserver watch object the client re-parses",
	)

	// The multiplexer's query string must reach WatchKube (decoded into url.Values).
	assert.Equal(t, path, stub.path())
	assert.Equal(t, "app=nginx", stub.query().Get("labelSelector"))

	// A CLOSE for the same {clusterId, path, query} key tears down the watch: the upstream reader closes.
	sendWSMux(
		t,
		conn,
		wsMuxMessage{ClusterID: cluster, Path: path, Query: query, UserID: "u1", Type: "CLOSE"},
	)

	require.Eventually(t, closed.Load, 2*time.Second, 10*time.Millisecond,
		"CLOSE must cancel the subscription and close its apiserver watch")
}

// TestWSMultiplexerTearsDownOnDisconnect asserts that dropping the client connection cancels every live
// watch (no watch outlives the socket).
func TestWSMultiplexerTearsDownOnDisconnect(t *testing.T) {
	t.Parallel()

	reader, _ := io.Pipe()
	closed := &atomic.Bool{}
	stub := &pipeWatchStub{
		reader: &closeTrackingReader{ReadCloser: reader, closed: closed},
		closed: closed,
	}

	server := httptest.NewServer((&api.Server{Service: stub}).Handler())
	defer server.Close()

	conn, _ := dialWSMux(t, server.URL)

	sendWSMux(
		t,
		conn,
		wsMuxMessage{ClusterID: "c1", Path: "/api/v1/pods", UserID: "u1", Type: "REQUEST"},
	)

	// Wait until the watch is open (WatchKube has captured the path), then abruptly close the client.
	require.Eventually(
		t,
		func() bool { return stub.path() != "" },
		2*time.Second,
		10*time.Millisecond,
	)

	_ = conn.Close(websocket.StatusNormalClosure, "")

	require.Eventually(t, closed.Load, 2*time.Second, 10*time.Millisecond,
		"client disconnect must cancel the subscription and close its apiserver watch")
}

// TestWSMultiplexerErrorsOnUnsupportedType asserts an unknown message type yields a Headlamp ERROR frame
// whose data carries an {"error": ...} object (what the client parses).
func TestWSMultiplexerErrorsOnUnsupportedType(t *testing.T) {
	t.Parallel()

	reader, _ := io.Pipe()
	closed := &atomic.Bool{}
	stub := &pipeWatchStub{
		reader: &closeTrackingReader{ReadCloser: reader, closed: closed},
		closed: closed,
	}

	server := httptest.NewServer((&api.Server{Service: stub}).Handler())
	defer server.Close()

	conn, cleanup := dialWSMux(t, server.URL)
	defer cleanup()

	sendWSMux(
		t,
		conn,
		wsMuxMessage{ClusterID: "c1", Path: "/api/v1/pods", UserID: "u1", Type: "BOGUS"},
	)

	msg := readWSMux(t, conn)
	assert.Equal(t, "ERROR", msg.Type)

	var payload struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(msg.Data), &payload))
	assert.Contains(t, payload.Error, "unsupported message type")
}

// closeTrackingReader records when Close is called so a test can assert a watch was torn down. It is
// idempotent (multiple Close calls are safe) so both the ctx-driven closer and the handler's defer can
// close it without erroring.
type closeTrackingReader struct {
	io.ReadCloser

	closed *atomic.Bool
}

func (c *closeTrackingReader) Close() error {
	c.closed.Store(true)

	err := c.ReadCloser.Close()
	if err != nil {
		return fmt.Errorf("close tracking reader: %w", err)
	}

	return nil
}
