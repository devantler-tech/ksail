package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// defaultEventsInterval is how often the events stream re-checks the backend for changes when the
	// Server does not override it (EventsInterval). It is chosen to feel live without hammering
	// provider discovery on the local backend; the client opens one persistent connection instead of
	// polling, and the stream only sends a payload when the cluster list actually changes.
	defaultEventsInterval = 5 * time.Second

	// clustersEvent names the SSE frame carrying the full cluster list.
	clustersEvent = "clusters"
	// streamErrorEvent names the SSE frame carrying a backend List failure. It is intentionally NOT
	// "error": EventSource reserves the "error" event type for connection-level failures, so a server
	// frame literally named "error" cannot be observed by an addEventListener("error", …) handler
	// without also catching reconnect errors. A distinct name lets the client consume it cleanly.
	streamErrorEvent = "stream-error"
	// heartbeatComment is the SSE comment sent on a quiet tick to keep the connection alive.
	heartbeatComment = "heartbeat"
)

// errStreamingUnsupported is returned when the ResponseWriter cannot stream (no http.Flusher). In
// practice every net/http server ResponseWriter flushes; this guards the rare wrapper that does not.
var errStreamingUnsupported = errors.New("streaming not supported by the server")

// eventsInterval returns the configured re-check interval, or the default when unset.
func (s *Server) eventsInterval() time.Duration {
	if s.EventsInterval > 0 {
		return s.EventsInterval
	}

	return defaultEventsInterval
}

// handleEvents streams Server-Sent Events (SSE). It is the shared streaming transport the web UI uses
// for live updates: today it pushes a "clusters" event with the full cluster list (once on connect,
// then whenever the serialized list changes), and emits a heartbeat comment on every quiet tick so
// the connection and any intermediary proxy stay alive. The stream ends when the client disconnects
// (request context cancelled) or the server shuts down.
//
// GET is non-mutating, so the read-only guard permits it; the path is under /api/, so the auth guard
// requires a session when OIDC is enabled. Cookies (the OIDC session) flow on the EventSource
// connection automatically, so no extra wiring is needed. Because the connection is long-lived, the
// session is re-validated each tick (see sessionValid) so it honours expiry mid-stream rather than
// streaming past SessionTTL.
func (s *Server) handleEvents(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeError(writer, http.StatusInternalServerError, errStreamingUnsupported)

		return
	}

	setSSEHeaders(writer)
	writer.WriteHeader(http.StatusOK)

	flusher.Flush()

	ctx := request.Context()

	// Emit an initial snapshot immediately so the client renders without waiting a full interval.
	last := s.writeClusterEvent(ctx, writer, "")

	flusher.Flush()

	ticker := time.NewTicker(s.eventsInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Stop streaming once the session expires; the client's EventSource reconnects and is
			// forced back through authGuard, which returns 401 and surfaces the login screen.
			if !s.sessionValid(request) {
				return
			}

			last = s.writeClusterEvent(ctx, writer, last)

			flusher.Flush()
		}
	}
}

// sessionValid reports whether the request still carries a valid (unexpired) session. With auth
// disabled (the loopback/local backend) it is always true. The events stream checks it each tick so a
// long-lived connection honours session expiry mid-stream — matching the per-request validation the
// REST endpoints get from authGuard — instead of streaming indefinitely past SessionTTL.
func (s *Server) sessionValid(request *http.Request) bool {
	if s.auth == nil {
		return true
	}

	_, ok := s.auth.currentUser(request)

	return ok
}

// writeClusterEvent lists clusters and writes a "clusters" SSE event when the serialized list differs
// from last, otherwise a heartbeat comment so the connection stays alive. A List error is written as
// a "stream-error" event and last is returned unchanged — a transient discovery failure must not end
// the stream or blank the client's last-known-good list. It returns the latest payload observed so
// the caller can detect changes across ticks.
func (s *Server) writeClusterEvent(ctx context.Context, writer io.Writer, last string) string {
	list, err := s.Service.List(ctx)
	if err != nil {
		writeSSEEvent(writer, streamErrorEvent, errorPayload(err))

		return last
	}

	payload, err := json.Marshal(toFullClusterList(list))
	if err != nil {
		return last
	}

	if string(payload) == last {
		writeSSEComment(writer, heartbeatComment)

		return last
	}

	writeSSEEvent(writer, clustersEvent, string(payload))

	return string(payload)
}

// setSSEHeaders configures the response for an event stream: the SSE content type, no caching, and a
// hint disabling proxy buffering (nginx honours X-Accel-Buffering) so events flush promptly.
func setSSEHeaders(writer http.ResponseWriter) {
	header := writer.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
}

// writeSSEEvent writes one SSE frame: an "event:" line followed by one "data:" line per line of the
// payload (SSE requires multi-line data to be split across multiple data: fields), terminated by a
// blank line. Errors are ignored: a write failure means the client disconnected, which the handler's
// context cancellation already handles.
func writeSSEEvent(writer io.Writer, event, data string) {
	_, _ = fmt.Fprintf(writer, "event: %s\n", event)

	for line := range strings.SplitSeq(data, "\n") {
		_, _ = fmt.Fprintf(writer, "data: %s\n", line)
	}

	_, _ = io.WriteString(writer, "\n")
}

// writeSSEComment writes an SSE comment line (": text"), used as a heartbeat. Comments are ignored by
// EventSource but keep the connection from being reaped as idle.
func writeSSEComment(writer io.Writer, comment string) {
	_, _ = fmt.Fprintf(writer, ": %s\n\n", comment)
}

// errorPayload renders an error as the JSON the SPA expects ({"error":"..."}), matching the shape of
// the REST error responses so the client can parse stream errors with the same logic.
func errorPayload(err error) string {
	data, marshalErr := json.Marshal(map[string]string{errorJSONKey: err.Error()})
	if marshalErr != nil {
		return `{"error":"internal error"}`
	}

	return string(data)
}
