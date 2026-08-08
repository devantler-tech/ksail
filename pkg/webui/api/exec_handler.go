package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// execKeepaliveInterval is how often the exec session re-validates the auth session (honouring
	// OIDC expiry mid-stream, like the SSE handler) and pings the client.
	execKeepaliveInterval = 30 * time.Second
	// execPongWait is the read deadline; a missing pong (half-open client) past it tears the session
	// down so the goroutine and remote exec stream don't leak. It exceeds the ping interval so a live
	// client always refreshes the deadline in time.
	execPongWait = 70 * time.Second
	// execPingTimeout bounds a single ping write.
	execPingTimeout = 10 * time.Second
)

// execClientMessage is a frame from the browser terminal: stdin data or a resize event.
type execClientMessage struct {
	Op   string `json:"op"`
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// execServerMessage is a frame to the browser terminal: stdout data, an error, or session exit.
type execServerMessage struct {
	Op   string `json:"op"`
	Data string `json:"data,omitempty"`
}

// execWSWriter adapts a WebSocket connection to an io.Writer, emitting stdout frames. WriteJSON is not
// safe for concurrent use, so all writes are serialized through the mutex.
type execWSWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *execWSWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	err := w.conn.WriteJSON(execServerMessage{Op: "stdout", Data: string(data)})
	if err != nil {
		return 0, fmt.Errorf("write stdout frame: %w", err)
	}

	return len(data), nil
}

func (w *execWSWriter) send(op, data string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	_ = w.conn.WriteJSON(execServerMessage{Op: op, Data: data})
}

func (s *Server) handleExec(writer http.ResponseWriter, request *http.Request) {
	svc, ok := s.Service.(ExecService)
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	// Exec runs arbitrary commands in a workload, so it is refused in read-only mode. The read-only
	// guard does not catch it (the WebSocket upgrade is a GET), so the check lives here — sharing the
	// guard's response body so the SPA recognizes it.
	if s.ReadOnly {
		writeReadOnlyError(writer)

		return
	}

	query := request.URL.Query()
	if query.Get("pod") == "" {
		writeClientError(writer, fmt.Errorf("%w: pod is required", ErrInvalid))

		return
	}

	command := query["command"]
	if len(command) == 0 {
		command = []string{"/bin/sh"}
	}

	// Use gorilla/websocket's default origin policy: requests without an Origin header are allowed,
	// and browser requests must be same-origin (Origin host equals Host). The local UI is
	// unauthenticated and bound to loopback, so allowing arbitrary browser origins would let a
	// malicious page drive pod exec through a victim's local kubeconfig.
	upgrader := websocket.Upgrader{}

	conn, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return // Upgrade already wrote the error response.
	}

	defer func() { _ = conn.Close() }()

	s.runExecSession(request, conn, svc, ExecRequest{
		Namespace: query.Get("namespace"),
		Pod:       query.Get("pod"),
		Container: query.Get("container"),
		Command:   command,
	})
}

// runExecSession bridges the WebSocket connection to the ExecService: client frames feed stdin/resize,
// the executor's stdout streams back as frames. It returns when the session ends or the client leaves.
func (s *Server) runExecSession(
	request *http.Request,
	conn *websocket.Conn,
	svc ExecService,
	execRequest ExecRequest,
) {
	stdinReader, stdinWriter := io.Pipe()
	resize := make(chan TerminalSize, 1)
	output := &execWSWriter{conn: conn}

	sessionCtx, cancel := context.WithCancel(request.Context())
	defer cancel()

	go execReadPump(conn, stdinWriter, resize, cancel)
	go s.execWatchdog(sessionCtx, conn, request, cancel)

	err := svc.Exec(
		sessionCtx,
		request.PathValue("namespace"),
		request.PathValue("name"),
		execRequest,
		ExecStreams{Stdin: stdinReader, Stdout: output, Resize: resize},
	)
	if err != nil {
		output.send("error", err.Error())
	}

	output.send("exit", "")
}

// execReadPump reads client frames until the connection closes, writing stdin data into the pipe and
// delivering resize events. On exit it closes stdin (signalling EOF to the executor) and cancels the
// session.
func execReadPump(
	conn *websocket.Conn,
	stdin *io.PipeWriter,
	resize chan<- TerminalSize,
	cancel context.CancelFunc,
) {
	// A pong (sent by the client in reply to the watchdog's pings) refreshes the read deadline; if a
	// half-open client stops ponging, ReadJSON fails past execPongWait and the session tears down.
	_ = conn.SetReadDeadline(time.Now().Add(execPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(execPongWait))
	})

	defer func() {
		_ = stdin.Close()

		close(resize)
		cancel()
	}()

	for {
		var msg execClientMessage

		err := conn.ReadJSON(&msg)
		if err != nil {
			return
		}

		switch msg.Op {
		case "stdin":
			_, err = stdin.Write([]byte(msg.Data))
			if err != nil {
				return
			}
		case "resize":
			select {
			case resize <- TerminalSize{Rows: msg.Rows, Cols: msg.Cols}:
			default:
			}
		}
	}
}

// execWatchdog re-validates the auth session on a ticker (so an interactive shell honours OIDC expiry
// mid-stream rather than outliving SessionTTL, matching the SSE handler) and pings the client to keep
// the connection alive / detect a half-open peer. Cancelling tears down the exec stream.
func (s *Server) execWatchdog(
	ctx context.Context,
	conn *websocket.Conn,
	request *http.Request,
	cancel context.CancelFunc,
) {
	ticker := time.NewTicker(execKeepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.sessionValid(request) {
				cancel()

				return
			}

			err := conn.WriteControl(
				websocket.PingMessage,
				nil,
				time.Now().Add(execPingTimeout),
			)
			if err != nil {
				return
			}
		}
	}
}
