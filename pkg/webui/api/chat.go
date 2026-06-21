package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// chatEvent names the SSE frame carrying one streamed chat event (a JSON ChatEvent). A single frame
// name is used; the event's own Type field discriminates delta/tool/error/done, so the SPA parses one
// frame shape.
const chatEvent = "chat"

// errChatUnavailable is returned when a ChatService is wired but the assistant cannot run in this
// environment (e.g. GitHub Copilot is not configured). It maps to 501 so the SPA can distinguish "not
// available here" from a transient turn error.
var errChatUnavailable = fmt.Errorf("%w: the AI assistant is not configured", ErrNotSupported)

// ChatMessage is one prior turn in the conversation, sent so the backend can give the assistant
// context for the new message.
type ChatMessage struct {
	// Role is "user" or "assistant".
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is one chat turn from the SPA: the new user message, the prior history, and the cluster
// context the assistant should reason about (best-effort — a backend may ignore the scope).
type ChatRequest struct {
	Message   string        `json:"message"`
	History   []ChatMessage `json:"history,omitempty"`
	Cluster   string        `json:"cluster,omitempty"`
	Namespace string        `json:"namespace,omitempty"`
}

// ChatEventType classifies a streamed chat event.
type ChatEventType string

const (
	// ChatEventDelta carries an incremental chunk of the assistant's reply text.
	ChatEventDelta ChatEventType = "delta"
	// ChatEventTool reports a tool the assistant invoked (display-only, so the user sees activity).
	ChatEventTool ChatEventType = "tool"
	// ChatEventError carries a turn-level error message; the turn then ends.
	ChatEventError ChatEventType = "error"
	// ChatEventDone signals the turn completed normally.
	ChatEventDone ChatEventType = "done"
)

// ChatEvent is one streamed event of a chat turn, serialized into each SSE frame.
type ChatEvent struct {
	Type ChatEventType `json:"type"`
	Text string        `json:"text,omitempty"`
}

// ChatService is an optional interface a ClusterService may implement to power the web UI's AI
// assistant. The route POST /api/v1/chat is registered whenever a backend implements it, but the
// capability the SPA gates the assistant panel on (capabilities.aiChat) follows ChatAvailable — so a
// backend can advertise the interface yet report the assistant unavailable (e.g. Copilot not
// configured) without the panel appearing. Chat runs one turn for req, invoking emit for each streamed
// event until it returns; a normal completion ends with a ChatEventDone.
//
// The local `ksail ui`/desktop backend implements it over GitHub Copilot (reusing KSail's chat
// service); the operator leaves it unimplemented.
type ChatService interface {
	ChatAvailable(ctx context.Context) bool
	Chat(ctx context.Context, req ChatRequest, emit func(ChatEvent)) error
}

func (s *Server) handleChat(writer http.ResponseWriter, request *http.Request) {
	chatService, isChatService := s.Service.(ChatService)
	if !isChatService {
		writeClientError(writer, ErrNotSupported)

		return
	}

	var req ChatRequest

	// Decode before the SSE upgrade so a malformed body returns a normal JSON 400, not a stream frame.
	err := decodeJSON(writer, request, &req)
	if err != nil {
		return
	}

	if req.Message == "" {
		writeError(writer, http.StatusBadRequest, errEmptyChatMessage)

		return
	}

	// Availability is checked before the upgrade too, so "not configured" is a clean 501 the SPA can
	// surface distinctly from a mid-turn error frame.
	if !chatService.ChatAvailable(request.Context()) {
		writeClientError(writer, errChatUnavailable)

		return
	}

	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeError(writer, http.StatusInternalServerError, errStreamingUnsupported)

		return
	}

	setSSEHeaders(writer)
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()

	emit := func(event ChatEvent) {
		payload, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return
		}

		writeSSEEvent(writer, chatEvent, string(payload))
		flusher.Flush()
	}

	chatErr := chatService.Chat(request.Context(), req, emit)
	// A cancelled request (client disconnect) is not surfaced — the stream is already gone. Any other
	// error becomes a final error frame so the SPA shows why the turn stopped.
	if chatErr != nil && request.Context().Err() == nil {
		emit(ChatEvent{Type: ChatEventError, Text: chatErr.Error()})
	}
}

// errEmptyChatMessage is the 400 returned when a chat request carries no message text.
var errEmptyChatMessage = fmt.Errorf("%w: message is required", ErrInvalid)
