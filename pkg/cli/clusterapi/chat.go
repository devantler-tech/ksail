package clusterapi

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

// chatRunner runs one AI assistant turn, streaming events. It is the seam the local Service uses to
// satisfy api.ChatService without importing the Copilot adapter directly: the adapter (pkg/svc/webchat)
// is wired in by the `ksail ui` command via UseChat, which keeps the heavy Copilot dependency out of
// the core service and its tests. A nil runner means the assistant is unavailable.
type chatRunner interface {
	Available(ctx context.Context) bool
	Run(ctx context.Context, req api.ChatRequest, emit func(api.ChatEvent)) error
	// ConfirmTool resolves a pending write-tool confirmation issued during a turn (see api.ChatService).
	ConfirmTool(confirmID string, approved bool)
}

// UseChat wires the AI assistant backend (e.g. the Copilot-backed runner from pkg/svc/webchat). Until
// it is called the assistant reports unavailable, so the web UI hides the assistant panel and the chat
// endpoint returns 501 — graceful degradation for backends/environments without an assistant.
func (s *Service) UseChat(runner chatRunner) {
	s.chat = runner
}

// ChatAvailable reports whether the AI assistant can run (api.ChatService): a runner must be wired and
// report itself available (e.g. a Copilot token is configured).
func (s *Service) ChatAvailable(ctx context.Context) bool {
	return s.chat != nil && s.chat.Available(ctx)
}

// Chat runs one assistant turn (api.ChatService), delegating to the wired runner. It returns
// api.ErrNotSupported when no runner is wired; the HTTP handler checks ChatAvailable first, so this is
// the defensive path.
func (s *Service) Chat(ctx context.Context, req api.ChatRequest, emit func(api.ChatEvent)) error {
	if s.chat == nil {
		return fmt.Errorf("%w: AI assistant not configured", api.ErrNotSupported)
	}

	err := s.chat.Run(ctx, req, emit)
	if err != nil {
		return fmt.Errorf("run assistant turn: %w", err)
	}

	return nil
}

// ConfirmTool resolves a pending write-tool confirmation (api.ChatService), delegating to the wired
// runner. A nil runner (assistant unavailable) makes it a no-op, matching ConfirmTool's
// unknown-confirmId semantics — a stale decision after the runner is gone is harmless.
func (s *Service) ConfirmTool(confirmID string, approved bool) {
	if s.chat == nil {
		return
	}

	s.chat.ConfirmTool(confirmID, approved)
}
