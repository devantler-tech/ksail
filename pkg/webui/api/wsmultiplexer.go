package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	// wsMultiplexerPath is the route the Headlamp WebSocket multiplexer client connects to. Headlamp's
	// frontend builds `${baseWsUrl}/wsMultiplexer` (MULTIPLEXER_ENDPOINT = 'wsMultiplexer' in
	// frontend/src/lib/k8s/api/v2/multiplexer.ts), so the endpoint must live at the root, not under
	// /api/v1, for an unmodified plugin's WebSocketManager to find it.
	wsMultiplexerPath = "/wsMultiplexer"
	// wsMaxSubscriptions caps how many concurrent watch subscriptions one client connection may open, so
	// a single socket cannot fan out to an unbounded number of apiserver watches (each pins a goroutine
	// and an upstream connection).
	wsMaxSubscriptions = 256
	// wsIdleTimeout bounds a client connection that sends nothing (no REQUEST/CLOSE) and whose watches
	// emit nothing: if neither a client message nor a watch event is seen for this long the connection is
	// closed. A live client keeps it open implicitly (every watch event resets the deadline). It mirrors
	// the SSE handler's watchIdleTimeout.
	wsIdleTimeout = 5 * time.Minute
	// wsReadLimit caps a single inbound client frame (a REQUEST/CLOSE control message is tiny); it stops a
	// pathological client from buffering an unbounded message.
	wsReadLimit = 1 << 16 // 64 KiB
	// wsWriteTimeout bounds a single frame write so a stuck or slow client cannot block a watch goroutine
	// (or the COMPLETE/ERROR senders) indefinitely; on timeout the write fails and the subscription ends.
	wsWriteTimeout = 10 * time.Second
	// wsMessageRequest is the client message type that starts watching a resource (Headlamp's "REQUEST").
	wsMessageRequest = "REQUEST"
	// wsMessageClose is the client message type that stops watching a resource (Headlamp's "CLOSE").
	wsMessageClose = "CLOSE"
	// wsMessageData wraps one apiserver watch event sent back to the client (Headlamp's "DATA"). The
	// client (multiplexer.ts handleWebSocketMessage) JSON-parses the `data` field into the watch object.
	wsMessageData = "DATA"
	// wsMessageComplete tells the client a subscription's stream ended (Headlamp's "COMPLETE"); the client
	// records the path as completed.
	wsMessageComplete = "COMPLETE"
	// wsMessageError reports a subscription-level failure to the client (Headlamp's "ERROR"); the client
	// JSON-parses the `data` field and reads its `error` key.
	wsMessageError = "ERROR"
)

// errWSSubscriptionLimit is returned to a client that exceeds wsMaxSubscriptions on one connection.
var errWSSubscriptionLimit = errors.New("subscription limit reached for this connection")

// errWSUnsupportedType is returned for a control frame whose type is neither REQUEST nor CLOSE; it is
// echoed to the client as an ERROR frame, mirroring Headlamp's "unsupported message type" response.
var errWSUnsupportedType = errors.New("unsupported message type")

// wsMessage is the multiplexer wire frame, byte-compatible with Headlamp's backend Message
// (backend/cmd/multiplexer.go) and the frontend WebSocketMessage (multiplexer.ts). The same struct
// carries client→server control messages (REQUEST/CLOSE) and server→client frames (DATA/COMPLETE/ERROR);
// omitempty on data/binary matches Headlamp so a control message serialises identically.
type wsMessage struct {
	// ClusterID routes the message to a cluster. KSail maps it to the cluster name (the {name} the REST
	// resource browser uses); an empty ClusterID falls back to the path's implicit default.
	ClusterID string `json:"clusterId"`
	// Path is the apiserver collection path to watch (e.g. "/api/v1/pods"), exactly as Headlamp's client
	// derives it from the resource URL's pathname.
	Path string `json:"path"`
	// Query is the apiserver query string (without the leading '?'), e.g. a labelSelector. It is part of
	// the subscription key, so two watches on the same path with different queries are distinct.
	Query string `json:"query"`
	// UserID is Headlamp's per-user identifier. KSail authenticates the whole connection (the auth guard /
	// session cookie), so UserID is only used to key/echo the subscription, not for authorization.
	UserID string `json:"userId"`
	// Data carries the payload: on a DATA frame it is one apiserver watch event's JSON; on an ERROR frame
	// it is a JSON object with an "error" key. Empty on control and COMPLETE messages.
	Data string `json:"data,omitempty"`
	// Binary flags a base64-encoded binary payload in Headlamp; KSail watch events are always JSON text,
	// so it is always false here (kept for wire compatibility).
	Binary bool `json:"binary,omitempty"`
	// Type is the message type: REQUEST/CLOSE from the client, DATA/COMPLETE/ERROR from the server.
	Type string `json:"type"`
}

// wsSubscription is one live watch opened for a REQUEST. Cancelling ctx tears down the apiserver watch
// and its streaming goroutine; the WaitGroup lets the connection wait for the goroutine to finish on
// cleanup so no write races a closed socket.
type wsSubscription struct {
	cancel context.CancelFunc
}

// handleWSMultiplexer accepts a Headlamp-multiplexer WebSocket and serves its wire protocol: it reads
// REQUEST/CLOSE control frames, backs each REQUEST with one read-only apiserver WATCH (via WatchKube),
// and streams every watch event back as a DATA frame keyed by {clusterId, path, query} so the client's
// WebSocketManager routes it to the right subscription. All watches are torn down when the client
// disconnects. It is a GET upgrade and read-only (watches do not mutate), so the read-only guard does
// not apply; the connection is authenticated by the same session the auth guard enforces on the
// upgrade request.
func (s *Server) handleWSMultiplexer(writer http.ResponseWriter, request *http.Request) {
	watcher, ok := s.Service.(KubeWatch)
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	conn, err := websocket.Accept(writer, request, nil)
	if err != nil {
		// Accept already wrote an HTTP error response on failure; nothing more to do.
		return
	}

	conn.SetReadLimit(wsReadLimit)

	manager := &wsConnManager{
		server:        s,
		watcher:       watcher,
		conn:          conn,
		baseCtx:       request.Context(),
		subscriptions: map[string]*wsSubscription{},
	}
	defer manager.closeAll()

	manager.run(request)
}

// wsConnManager owns the per-connection subscription set and serialises all writes to the socket (a
// coder/websocket connection allows only one concurrent writer). Each watch goroutine sends frames
// through writeMessage, which holds writeMu, so concurrent watches never interleave a partial frame.
type wsConnManager struct {
	server  *Server
	watcher KubeWatch
	conn    *websocket.Conn
	// baseCtx is the connection's request context. COMPLETE/ERROR frames derive their write context from
	// it (a subscription's own context may already be cancelled when they fire), so a write is still bound
	// to the connection's lifetime rather than an unrelated background context.
	baseCtx context.Context //nolint:containedctx // the connection's lifetime context, used for late writes.

	writeMu sync.Mutex

	mu            sync.Mutex
	subscriptions map[string]*wsSubscription
	wg            sync.WaitGroup
}

// run reads client control frames until the connection ends (client disconnect, idle timeout, or a read
// error). The read deadline is refreshed on every client frame and every watch event (writeMessage), so
// an actively-watched connection is never reaped while a quiet, silent one is.
func (m *wsConnManager) run(request *http.Request) {
	baseCtx := request.Context()

	for {
		ctx, cancel := context.WithTimeout(baseCtx, wsIdleTimeout)
		typ, data, err := m.conn.Read(ctx)

		cancel()

		if err != nil {
			return
		}

		if typ != websocket.MessageText {
			continue
		}

		msg, decodeErr := decodeControlFrame(data)
		if decodeErr != nil {
			// A malformed control frame is ignored (no routable subscription context), matching Headlamp's
			// backend, which logs and continues rather than closing the connection.
			continue
		}

		m.handleClientMessage(baseCtx, request, msg)
	}
}

// handleClientMessage dispatches one decoded control frame. CLOSE tears down the matching subscription;
// REQUEST opens a new watch (idempotent per key). Any other type is reported back as an ERROR frame for
// that key, mirroring Headlamp's "unsupported message type" response.
func (m *wsConnManager) handleClientMessage(
	ctx context.Context,
	request *http.Request,
	msg wsMessage,
) {
	switch msg.Type {
	case wsMessageClose:
		m.unsubscribe(msg)
	case wsMessageRequest:
		err := m.subscribe(ctx, request, msg)
		if err != nil {
			m.sendError(msg, err)
		}
	default:
		m.sendError(msg, fmt.Errorf("%w: %s", errWSUnsupportedType, msg.Type))
	}
}

// subscribe opens one apiserver WATCH for the REQUEST and streams its events back as DATA frames. It is
// idempotent: a REQUEST for a key that is already subscribed is a no-op (Headlamp's client may resend on
// reconnect). It enforces the per-connection subscription cap before opening the watch.
func (m *wsConnManager) subscribe(ctx context.Context, request *http.Request, msg wsMessage) error {
	key := wsSubscriptionKey(msg)

	m.mu.Lock()
	if _, exists := m.subscriptions[key]; exists {
		m.mu.Unlock()

		return nil
	}

	if len(m.subscriptions) >= wsMaxSubscriptions {
		m.mu.Unlock()

		return errWSSubscriptionLimit
	}

	watchCtx, cancel := context.WithCancel(ctx)
	m.subscriptions[key] = &wsSubscription{cancel: cancel}
	m.wg.Add(1)
	m.mu.Unlock()

	stream, err := m.watcher.WatchKube(
		watchCtx,
		request.PathValue("namespace"),
		msg.ClusterID,
		msg.Path,
		parseWSQuery(msg.Query),
	)
	if err != nil {
		cancel()
		m.removeSubscription(key)
		m.wg.Done()

		return fmt.Errorf("open watch: %w", err)
	}

	go m.streamSubscription(watchCtx, key, msg, stream)

	return nil
}

// streamSubscription scans the apiserver watch stream and emits one DATA frame per watch event until the
// stream ends, the subscription is cancelled (CLOSE/disconnect), or the context is done. A COMPLETE frame
// is sent when the stream ends naturally, matching Headlamp's per-subscription completion signal.
func (m *wsConnManager) streamSubscription(
	ctx context.Context,
	key string,
	msg wsMessage,
	stream io.ReadCloser,
) {
	defer m.wg.Done()
	defer func() { _ = stream.Close() }()
	defer m.removeSubscription(key)

	lines, done := scanLinesToChannel(ctx, stream, watchMaxLineBytes)

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			m.sendComplete(msg)

			return
		case line := <-lines:
			err := m.writeMessage(ctx, m.dataMessage(msg, line))
			if err != nil {
				return
			}
		}
	}
}

// dataMessage frames one apiserver watch event line as a DATA message echoing the subscription's routing
// fields, so the client's WebSocketManager keys it back to the right listener (createKey builds
// `${clusterId}:${path}:${query}`). The raw line IS the JSON the client re-parses out of `data`.
func (m *wsConnManager) dataMessage(msg wsMessage, line string) wsMessage {
	return wsMessage{
		ClusterID: msg.ClusterID,
		Path:      msg.Path,
		Query:     msg.Query,
		UserID:    msg.UserID,
		Data:      line,
		Type:      wsMessageData,
	}
}

// sendComplete sends the COMPLETE frame for a subscription whose stream ended. It uses a fresh base
// context (the subscription's own context may already be done at this point), letting writeMessage apply
// the write deadline.
func (m *wsConnManager) sendComplete(msg wsMessage) {
	_ = m.writeMessage(m.baseCtx, wsMessage{
		ClusterID: msg.ClusterID,
		Path:      msg.Path,
		Query:     msg.Query,
		UserID:    msg.UserID,
		Type:      wsMessageComplete,
	})
}

// sendError sends an ERROR frame for a subscription. The error text is wrapped in a JSON object under
// "error" so the client (multiplexer.ts) parses data and reads data.error, exactly as Headlamp frames it.
func (m *wsConnManager) sendError(msg wsMessage, cause error) {
	payload, marshalErr := json.Marshal(map[string]string{"error": cause.Error()})
	if marshalErr != nil {
		payload = []byte(`{"error":"internal error"}`)
	}

	_ = m.writeMessage(m.baseCtx, wsMessage{
		ClusterID: msg.ClusterID,
		Path:      msg.Path,
		Query:     msg.Query,
		UserID:    msg.UserID,
		Data:      string(payload),
		Type:      wsMessageError,
	})
}

// writeMessage serialises one frame and writes it under writeMu (coder/websocket permits a single
// concurrent writer). It bounds the write with wsWriteTimeout so a stuck client cannot block a watch
// goroutine forever.
func (m *wsConnManager) writeMessage(ctx context.Context, msg wsMessage) error {
	encoded, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal ws message: %w", err)
	}

	writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()

	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	err = m.conn.Write(writeCtx, websocket.MessageText, encoded)
	if err != nil {
		return fmt.Errorf("write ws message: %w", err)
	}

	return nil
}

// unsubscribe cancels and removes the subscription matching a CLOSE message's key (no-op if unknown),
// tearing down its apiserver watch.
func (m *wsConnManager) unsubscribe(msg wsMessage) {
	m.cancelSubscription(wsSubscriptionKey(msg))
}

// removeSubscription drops a key once its streaming goroutine exits (stream end/error), so a later
// REQUEST for the same key opens a fresh watch.
func (m *wsConnManager) removeSubscription(key string) {
	m.cancelSubscription(key)
}

// cancelSubscription atomically removes a key from the subscription set and cancels its context (which
// tears down the apiserver watch and its streaming goroutine). It is a no-op for an unknown key, so a
// duplicate CLOSE or a CLOSE racing the goroutine's own cleanup is harmless.
func (m *wsConnManager) cancelSubscription(key string) {
	m.mu.Lock()

	sub, found := m.subscriptions[key]
	if found {
		delete(m.subscriptions, key)
	}

	m.mu.Unlock()

	if found {
		sub.cancel()
	}
}

// closeAll cancels every live subscription and waits for their goroutines to finish, then closes the
// socket. Called on handler return (client disconnect), so no watch outlives the connection.
func (m *wsConnManager) closeAll() {
	m.mu.Lock()
	for key, sub := range m.subscriptions {
		sub.cancel()
		delete(m.subscriptions, key)
	}
	m.mu.Unlock()

	m.wg.Wait()
	_ = m.conn.Close(websocket.StatusNormalClosure, "")
}

// wsSubscriptionKey builds the same correlation key Headlamp's client uses
// (createKey: `${clusterId}:${path}:${query}`), so a CLOSE matches the REQUEST that opened the watch and
// DATA frames echo fields that re-key to the originating listener. UserID is intentionally excluded —
// the frontend key omits it.
func wsSubscriptionKey(msg wsMessage) string {
	return fmt.Sprintf("%s:%s:%s", msg.ClusterID, msg.Path, msg.Query)
}

// decodeControlFrame unmarshals one client control frame into a wsMessage.
func decodeControlFrame(data []byte) (wsMessage, error) {
	var msg wsMessage

	err := json.Unmarshal(data, &msg)
	if err != nil {
		return wsMessage{}, fmt.Errorf("decode control frame: %w", err)
	}

	return msg, nil
}

// parseWSQuery turns a Headlamp query string (e.g. "labelSelector=app%3Dnginx") into url.Values for
// WatchKube. A malformed query yields empty values rather than failing the subscription (WatchKube forces
// watch=true regardless).
func parseWSQuery(query string) url.Values {
	values, err := url.ParseQuery(query)
	if err != nil {
		return url.Values{}
	}

	return values
}
