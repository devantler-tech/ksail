package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
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
	// ChatEventToolConfirm asks the user to approve or deny a write tool before it runs. It carries a
	// ConfirmID the SPA echoes back via POST /api/v1/chat/confirm; the turn blocks until the decision
	// arrives. Text is the tool name and Summary a short description of what it will do.
	ChatEventToolConfirm ChatEventType = "tool-confirm"
	// ChatEventError carries a turn-level error message; the turn then ends.
	ChatEventError ChatEventType = "error"
	// ChatEventDone signals the turn completed normally.
	ChatEventDone ChatEventType = "done"
)

// ChatEvent is one streamed event of a chat turn, serialized into each SSE frame.
type ChatEvent struct {
	Type ChatEventType `json:"type"`
	Text string        `json:"text,omitempty"`
	// ConfirmID identifies a pending write-tool confirmation (ChatEventToolConfirm only); the SPA
	// echoes it back to approve or deny the action.
	ConfirmID string `json:"confirmId,omitempty"`
	// Summary is a short, display-only description of a pending write tool (ChatEventToolConfirm only),
	// so the confirmation card can explain what the action does.
	Summary string `json:"summary,omitempty"`
}

// ChatConfirmRequest is the SPA's decision on a pending write-tool confirmation (POST
// /api/v1/chat/confirm): the ConfirmID from the tool-confirm event and whether the user approved it.
type ChatConfirmRequest struct {
	ConfirmID string `json:"confirmId"`
	Approved  bool   `json:"approved"`
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
//
// Write tools are gated: Chat may emit a ChatEventToolConfirm and block until the SPA posts the
// matching decision back, which the handler routes to ConfirmTool. ConfirmTool is safe to call with an
// unknown confirmId (a no-op), so a stale or duplicate decision is harmless.
type ChatService interface {
	ChatAvailable(ctx context.Context) bool
	Chat(ctx context.Context, req ChatRequest, emit func(ChatEvent)) error
	// ConfirmTool resolves a pending write-tool confirmation: approved is the user's decision for the
	// action identified by confirmID. An unknown confirmID is ignored.
	ConfirmTool(confirmID string, approved bool)
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

	// emit serializes frame writes: the assistant streams reply events from one goroutine while a
	// write-tool confirmation can be emitted from another (the Copilot SDK runs permission handlers off
	// the event-delivery goroutine), so without the lock the two could interleave writes to the same
	// ResponseWriter.
	var emitMu sync.Mutex

	emit := func(event ChatEvent) {
		payload, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return
		}

		emitMu.Lock()
		defer emitMu.Unlock()

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

// errEmptyConfirmID is the 400 returned when a confirm request omits the confirmId.
var errEmptyConfirmID = fmt.Errorf("%w: confirmId is required", ErrInvalid)

// handleConfirm resolves a pending write-tool confirmation: it decodes the SPA's decision and routes it
// to the chat service's ConfirmTool, which wakes the blocked turn so the tool proceeds or is rejected.
// Returning 204 (not a stream) because the decision is fire-and-forget; the outcome surfaces on the
// open chat SSE stream from the original turn.
func (s *Server) handleConfirm(writer http.ResponseWriter, request *http.Request) {
	chatService, isChatService := s.Service.(ChatService)
	if !isChatService {
		writeClientError(writer, ErrNotSupported)

		return
	}

	var req ChatConfirmRequest

	err := decodeJSON(writer, request, &req)
	if err != nil {
		return
	}

	if req.ConfirmID == "" {
		writeError(writer, http.StatusBadRequest, errEmptyConfirmID)

		return
	}

	chatService.ConfirmTool(req.ConfirmID, req.Approved)

	writer.WriteHeader(http.StatusNoContent)
}
