package api

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	// logSessionCheckInterval is how often a streaming log session re-validates the auth session, so it
	// honours OIDC expiry mid-stream rather than streaming past SessionTTL (matching handleEvents).
	logSessionCheckInterval = 30 * time.Second
	// logDefaultTailLines bounds the initial backlog when the client does not specify a tail, so opening
	// the viewer on a chatty pod doesn't dump its entire history.
	logDefaultTailLines = 1000
	// logMaxLineBytes caps a single log line so a pathological line can't exhaust memory.
	logMaxLineBytes = 1 << 20 // 1 MiB
)

func (s *Server) handleLogs(writer http.ResponseWriter, request *http.Request) {
	svc, ok := s.Service.(LogService)
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	flusher, flushable := writer.(http.Flusher)
	if !flushable {
		writeError(writer, http.StatusInternalServerError, errStreamingUnsupported)

		return
	}

	query := request.URL.Query()
	if query.Get("pod") == "" {
		writeClientError(writer, fmt.Errorf("%w: pod is required", ErrInvalid))

		return
	}

	// tail bounds the initial backlog. An unset/invalid value uses the default; tail=0 streams from the
	// start (the whole backlog), per the LogService contract (TailLines is applied only when > 0).
	tail := int64(logDefaultTailLines)

	parsed, parseErr := strconv.ParseInt(query.Get("tail"), 10, 64)
	if parseErr == nil && parsed >= 0 {
		tail = parsed
	}

	follow, _ := strconv.ParseBool(query.Get("follow"))

	// Resolve the stream before writing SSE headers, so a failure (e.g. pod not found) surfaces as a
	// normal JSON error response instead of an empty event stream.
	stream, err := svc.PodLogs(
		request.Context(),
		request.PathValue("namespace"),
		request.PathValue("name"),
		LogRequest{
			Namespace: query.Get("namespace"),
			Pod:       query.Get("pod"),
			Container: query.Get("container"),
			Follow:    follow,
			TailLines: tail,
		},
	)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	defer func() { _ = stream.Close() }()

	setSSEHeaders(writer)
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()

	s.streamLogLines(request, writer, flusher, stream)
}

// streamLogLines frames the raw log stream as SSE "log" events until the stream ends ("eof"), the
// client disconnects (request context), or the session expires (re-checked on a ticker). The blocking
// scan runs in a goroutine so the ticker and context cancellation stay responsive.
func (s *Server) streamLogLines(
	request *http.Request,
	writer io.Writer,
	flusher http.Flusher,
	stream io.Reader,
) {
	ctx := request.Context()
	lines := make(chan string)
	done := make(chan struct{})

	go func() {
		defer close(done)

		scanner := bufio.NewScanner(stream)
		scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), logMaxLineBytes)

		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()

	ticker := time.NewTicker(logSessionCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			writeSSEEvent(writer, "eof", "")
			flusher.Flush()

			return
		case line := <-lines:
			writeSSEEvent(writer, "log", line)
			flusher.Flush()
		case <-ticker.C:
			if !s.sessionValid(request) {
				return
			}
			// Keep an idle follow stream alive (a silent pod emits no lines), matching handleEvents.
			writeSSEComment(writer, heartbeatComment)
			flusher.Flush()
		}
	}
}
