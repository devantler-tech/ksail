// Package webchat powers the KSail web UI's AI assistant over GitHub Copilot. It reuses KSail's chat
// service (system context + Cobra-generated tools) and the Copilot SDK's streaming session, adapting
// them to the web transport's streaming contract (api.ChatService, via the local backend's runner
// seam): one turn per request, streamed as api.ChatEvent values.
//
// The assistant is READ-ONLY here: any tool that requires write permission is denied (a web request
// has no interactive consent channel, unlike the CLI). Availability requires a non-interactive Copilot
// token (KSAIL_COPILOT_TOKEN / COPILOT_TOKEN), since a server cannot drive the interactive device
// login.
//
// Runtime verification note: the live path spawns the Copilot CLI subprocess and streams from it, so it
// can only be exercised in a Copilot-configured environment — it is gated off (Available == false) when
// no token is present, which is the case in CI. Tests cover the non-Copilot logic (availability, prompt
// building); the streaming integration is verified manually with Copilot configured. This mirrors how
// the repo treats other CI-unverifiable integrations (cloud providers) — gated behind availability.
package webchat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	chatsvc "github.com/devantler-tech/ksail/v7/pkg/svc/chat"
	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
	"github.com/spf13/cobra"
)

const (
	// turnTimeout bounds a single chat turn so a stuck session cannot hold a request open forever.
	turnTimeout = 5 * time.Minute
	// sessionIdleTimeoutSeconds matches the CLI: idle Copilot sessions are reaped after 30 minutes.
	sessionIdleTimeoutSeconds = 1800
)

var (
	// errTurnTimeout is returned when a turn exceeds turnTimeout.
	errTurnTimeout = errors.New("assistant response timed out")
	// errAssistant wraps a session-level error message reported by the assistant.
	errAssistant = errors.New("assistant error")
)

// Runner powers the web UI's AI assistant. The Copilot client is started lazily on the first turn and
// reused across turns (starting it spawns a subprocess and authenticates, which is too slow per-turn);
// Close stops it. A fresh session is created per turn, so prior turns are replayed via the prompt.
type Runner struct {
	rootCmd *cobra.Command

	mu     sync.Mutex
	client *copilot.Client
}

// New builds a Runner that generates its tools from rootCmd's command tree (the same source the CLI
// chat uses), so the assistant can run the same read operations.
func New(rootCmd *cobra.Command) *Runner {
	return &Runner{rootCmd: rootCmd}
}

// Available reports whether the assistant can run: a Copilot token must be present. A server cannot
// complete the interactive device-login flow, so token auth is the only supported path here.
func (r *Runner) Available(_ context.Context) bool {
	return copilotToken() != ""
}

// Close stops the Copilot client (and its subprocess) if one was started.
func (r *Runner) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.client != nil {
		_ = r.client.Stop()
		r.client = nil
	}
}

// Run runs one chat turn for req, streaming the assistant's reply to emit and ending with a done event
// on normal completion. It is read-only: tools requiring write permission are denied.
func (r *Runner) Run(ctx context.Context, req api.ChatRequest, emit func(api.ChatEvent)) error {
	client, err := r.ensureClient(ctx)
	if err != nil {
		return err
	}

	tools, _ := chatsvc.GetKSailToolMetadata(r.rootCmd, nil, toolgen.NewSessionLogRef())
	streaming := true

	session, err := client.CreateSession(ctx, &copilot.SessionConfig{
		Streaming: &streaming,
		SystemMessage: &copilot.SystemMessageConfig{
			Mode:     "customize",
			Sections: chatsvc.BuildSystemSections(r.rootCmd),
		},
		Tools:               tools,
		OnPermissionRequest: readOnlyPermissionHandler(),
	})
	if err != nil {
		return fmt.Errorf("create chat session: %w", err)
	}

	defer func() { _ = session.Disconnect() }()

	return streamTurn(ctx, session, buildPrompt(req), emit)
}

// ensureClient starts the Copilot client once and returns the shared instance.
func (r *Runner) ensureClient(ctx context.Context) (*copilot.Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.client != nil {
		return r.client, nil
	}

	client := copilot.NewClient(&copilot.ClientOptions{
		LogLevel:                  "error",
		GitHubToken:               copilotToken(),
		SessionIdleTimeoutSeconds: sessionIdleTimeoutSeconds,
	})

	err := client.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("start Copilot client: %w", err)
	}

	r.client = client

	return client, nil
}

// copilotToken returns the configured Copilot token, in precedence order. GITHUB_TOKEN is intentionally
// excluded (it may lack Copilot scopes), matching the CLI's token resolution.
func copilotToken() string {
	for _, name := range []string{"KSAIL_COPILOT_TOKEN", "COPILOT_TOKEN"} {
		if token := os.Getenv(name); token != "" {
			return token
		}
	}

	return ""
}

// streamTurn sends the prompt and relays the session's streamed events to emit until the turn idles,
// errors, the context is cancelled, or the turn times out.
func streamTurn(
	ctx context.Context,
	session *copilot.Session,
	prompt string,
	emit func(api.ChatEvent),
) error {
	done := make(chan struct{})

	var once sync.Once

	finish := func() { once.Do(func() { close(done) }) }
	errCh := make(chan error, 1)

	unsubscribe := session.On(func(event copilot.SessionEvent) {
		relaySessionEvent(event, emit, errCh, finish)
	})
	defer unsubscribe()

	_, err := session.Send(ctx, copilot.MessageOptions{Prompt: prompt})
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	timer := time.NewTimer(turnTimeout)
	defer timer.Stop()

	select {
	case <-done:
	case <-ctx.Done():
		_ = session.Abort(ctx)

		return fmt.Errorf("chat cancelled: %w", ctx.Err())
	case <-timer.C:
		_ = session.Abort(ctx)

		return errTurnTimeout
	}

	emit(api.ChatEvent{Type: api.ChatEventDone})

	select {
	case turnErr := <-errCh:
		return turnErr
	default:
		return nil
	}
}

// relaySessionEvent maps one Copilot session event to the web transport's events: assistant text
// deltas and tool-start notices stream to emit; idle ends the turn; a session error is captured and
// ends the turn. Other event types are ignored.
func relaySessionEvent(
	event copilot.SessionEvent,
	emit func(api.ChatEvent),
	errCh chan<- error,
	finish func(),
) {
	//nolint:exhaustive // Only the few event types the web assistant surfaces are handled.
	switch event.Type() {
	case copilot.SessionEventTypeAssistantMessageDelta:
		if data, ok := event.Data.(*copilot.AssistantMessageDeltaData); ok {
			emit(api.ChatEvent{Type: api.ChatEventDelta, Text: data.DeltaContent})
		}
	case copilot.SessionEventTypeToolExecutionStart:
		if data, ok := event.Data.(*copilot.ToolExecutionStartData); ok {
			emit(api.ChatEvent{Type: api.ChatEventTool, Text: data.ToolName})
		}
	case copilot.SessionEventTypeSessionIdle:
		finish()
	case copilot.SessionEventTypeSessionError:
		if data, ok := event.Data.(*copilot.SessionErrorData); ok {
			select {
			case errCh <- fmt.Errorf("%w: %s", errAssistant, data.Message):
			default:
			}
		}

		finish()
	}
}

// readOnlyPermissionHandler auto-approves read operations and denies everything else, so the web
// assistant can inspect the cluster but never mutate it (a web request has no interactive consent).
func readOnlyPermissionHandler() copilot.PermissionHandlerFunc {
	return func(
		request copilot.PermissionRequest, _ copilot.PermissionInvocation,
	) (rpc.PermissionDecision, error) {
		if chatsvc.IsReadOperation(request.Kind()) {
			return &rpc.PermissionDecisionApproveOnce{}, nil
		}

		return &rpc.PermissionDecisionReject{}, nil
	}
}

// buildPrompt assembles the turn's prompt: the active-cluster context (so the assistant scopes its
// reasoning), the prior conversation (a fresh session is created per turn, so history is replayed), and
// the new user message.
func buildPrompt(req api.ChatRequest) string {
	var builder strings.Builder

	if req.Cluster != "" {
		fmt.Fprintf(&builder, "Active cluster: %s", req.Cluster)

		if req.Namespace != "" {
			fmt.Fprintf(&builder, " (namespace %s)", req.Namespace)
		}

		builder.WriteString(".\n\n")
	}

	for _, message := range req.History {
		fmt.Fprintf(&builder, "%s: %s\n", message.Role, message.Content)
	}

	builder.WriteString(req.Message)

	return builder.String()
}
