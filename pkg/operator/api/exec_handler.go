package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
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
	// guard does not catch it (the WebSocket upgrade is a GET), so the check lives here — mirroring the
	// guard's response shape so the SPA recognizes it.
	if s.ReadOnly {
		writeJSON(writer, http.StatusForbidden, map[string]any{
			"readOnly": true,
			"reason":   "UI is configured read-only (GitOps-enforced)",
		})

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

	// CheckOrigin allows any origin: the endpoint is already behind the auth guard (when OIDC is on)
	// and the read-only check above, and the desktop connects cross-origin (wails:// → loopback).
	upgrader := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}

	conn, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return // Upgrade already wrote the error response.
	}
	defer func() { _ = conn.Close() }()

	s.runExecSession(
		request.Context(),
		conn,
		svc,
		request.PathValue("namespace"),
		request.PathValue("name"),
		ExecRequest{
			Namespace: query.Get("namespace"),
			Pod:       query.Get("pod"),
			Container: query.Get("container"),
			Command:   command,
		},
	)
}

// runExecSession bridges the WebSocket connection to the ExecService: client frames feed stdin/resize,
// the executor's stdout streams back as frames. It returns when the session ends or the client leaves.
func (s *Server) runExecSession(
	ctx context.Context,
	conn *websocket.Conn,
	svc ExecService,
	clusterNamespace, clusterName string,
	request ExecRequest,
) {
	stdinReader, stdinWriter := io.Pipe()
	resize := make(chan TerminalSize, 1)
	output := &execWSWriter{conn: conn}

	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go execReadPump(conn, stdinWriter, resize, cancel)

	err := svc.Exec(sessionCtx, clusterNamespace, clusterName, request, ExecStreams{
		Stdin:  stdinReader,
		Stdout: output,
		Resize: resize,
	})
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
