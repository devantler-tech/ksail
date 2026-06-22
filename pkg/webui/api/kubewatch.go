package api

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	// watchIdleTimeout bounds a quiet watch: if the apiserver sends nothing (no events, no heartbeat)
	// for this long the stream is closed so a wedged upstream connection cannot pin a goroutine forever.
	// The client's EventSource reconnects automatically, re-establishing a fresh watch.
	watchIdleTimeout = 5 * time.Minute
	// watchMaxDuration caps the total lifetime of a single watch stream regardless of activity, so a
	// long-lived connection is periodically recycled (re-listing on reconnect) rather than running
	// unbounded. EventSource reconnects transparently, so the client observes no interruption.
	watchMaxDuration = 30 * time.Minute
	// watchSessionCheckInterval is how often a streaming watch re-validates the auth session, so it
	// honours OIDC expiry mid-stream rather than streaming past SessionTTL (matching handleLogs).
	watchSessionCheckInterval = 30 * time.Second
	// watchMaxLineBytes caps a single watch JSON line so a pathological event can't exhaust memory.
	watchMaxLineBytes = 1 << 20 // 1 MiB
	// watchEvent names the SSE frame carrying one apiserver watch event (the raw watch JSON object).
	watchEvent = "watch"
)

// KubeWatch is an optional interface a ClusterService may implement to open a streaming WATCH against a
// cluster's kube-apiserver, so the SPA — and the Headlamp-compatible plugins' K8s data layer — can
// receive live incremental updates (ADDED/MODIFIED/DELETED) for arbitrary resource kinds instead of
// polling. It is the streaming analogue of KubeProxy: GET-only by design (a read-only window onto the
// apiserver, not a general passthrough), so it is not gated by the read-only guard (watches don't
// mutate).
//
// WatchKube forces watch=true in the query and returns the apiserver's newline-delimited watch JSON
// stream; the handler reframes each line as an SSE event and the caller closes the stream.
//
// Security note: like KubeProxy this broadens the API beyond the curated resource allowlist to
// arbitrary apiserver reads with the caller's credentials. It is implemented only on the loopback-bound
// local `ksail open web`/desktop backend (where the caller already controls the kubeconfig); the
// operator leaves it unimplemented, so the route is not registered and the capability stays false there.
type KubeWatch interface {
	WatchKube(
		ctx context.Context,
		namespace, name, apiPath string,
		query url.Values,
	) (io.ReadCloser, error)
}

// handleKubeWatch opens a kube-apiserver WATCH for the path's cluster and streams the apiserver's
// newline-delimited watch JSON to the client as Server-Sent Events — one "watch" event per watch object
// (ADDED/MODIFIED/DELETED), flushed as it arrives. The stream ends when the client disconnects (request
// context cancelled), the apiserver stream ends, the session expires, or the idle/max-duration bounds
// trip. GET is non-mutating, so the read-only guard permits it; the path is under /api/, so the auth
// guard requires a session when OIDC is enabled.
func (s *Server) handleKubeWatch(writer http.ResponseWriter, request *http.Request) {
	watcher, ok := s.Service.(KubeWatch)
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	flusher, flushable := writer.(http.Flusher)
	if !flushable {
		writeError(writer, http.StatusInternalServerError, errStreamingUnsupported)

		return
	}

	// Bound the watch's total lifetime so it is recycled even if the apiserver never stops sending.
	ctx, cancel := context.WithTimeout(request.Context(), watchMaxDuration)
	defer cancel()

	// Resolve the stream before writing SSE headers, so a failure (e.g. cluster not found) surfaces as a
	// normal JSON error response instead of an empty event stream.
	stream, err := watcher.WatchKube(
		ctx,
		request.PathValue("namespace"),
		request.PathValue("name"),
		request.PathValue("path"),
		request.URL.Query(),
	)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	defer func() { _ = stream.Close() }()

	setSSEHeaders(writer)
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()

	s.streamWatchEvents(ctx, request, writer, flusher, stream)
}

// streamWatchEvents frames the raw newline-delimited watch JSON as SSE "watch" events until the stream
// ends ("eof"), the client disconnects (ctx), the session expires, or the idle timeout trips (re-checked
// on a ticker). The blocking scan runs in a goroutine so the ticker and context cancellation stay
// responsive. Each line is one apiserver watch object; the client applies it incrementally.
func (s *Server) streamWatchEvents(
	ctx context.Context,
	request *http.Request,
	writer io.Writer,
	flusher http.Flusher,
	stream io.Reader,
) {
	lines, done := scanWatchLines(ctx, stream)

	ticker := time.NewTicker(watchSessionCheckInterval)
	defer ticker.Stop()

	// idle trips when no watch line arrives for watchIdleTimeout; reset on every line so an active stream
	// is never reaped. A wedged upstream (no events, no close) is closed once it fires.
	idle := time.NewTimer(watchIdleTimeout)
	defer idle.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			writeSSEEvent(writer, "eof", "")
			flusher.Flush()

			return
		case <-idle.C:
			return
		case line := <-lines:
			writeSSEEvent(writer, watchEvent, line)
			flusher.Flush()
			idle.Reset(watchIdleTimeout)
		case <-ticker.C:
			// Stop once the session expires; otherwise keep an idle watch alive (a quiet cluster emits no
			// events) with a heartbeat, matching handleLogs/handleEvents.
			if !s.sessionValid(request) {
				return
			}

			writeSSEComment(writer, heartbeatComment)
			flusher.Flush()
		}
	}
}

// scanWatchLines reads newline-delimited watch JSON from stream in a goroutine, emitting each line on
// the returned channel and closing done when the stream ends. The goroutine exits if ctx is cancelled
// (so a never-ending upstream cannot leak it once the client disconnects). Splitting the scan out of
// streamWatchEvents keeps the select loop within its complexity budget.
func scanWatchLines(ctx context.Context, stream io.Reader) (<-chan string, <-chan struct{}) {
	lines := make(chan string)
	done := make(chan struct{})

	go func() {
		defer close(done)

		scanner := bufio.NewScanner(stream)
		scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), watchMaxLineBytes)

		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()

	return lines, done
}
