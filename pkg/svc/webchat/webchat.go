// Package webchat powers the KSail web UI's provider-neutral AI assistant. It reuses KSail's chat
// service (system context + Cobra-generated tools) and a shared streaming runtime, adapting
// them to the web transport's streaming contract (api.ChatService, via the local backend's runner
// seam): one turn per request, streamed as api.ChatEvent values.
//
// Write operations are gated by per-action confirmation: a tool that requires write permission is not
// auto-approved (a web request has no interactive consent channel like the CLI's TUI). Instead the
// handler emits a tool-confirm event carrying a unique confirmId and blocks until the SPA posts a
// decision back (POST /api/v1/chat/confirm → ConfirmTool), then approves or denies accordingly. Read
// operations are still auto-approved so the assistant can inspect the cluster without prompting.
// Copilot availability requires a non-interactive token because a server cannot drive device login.
// BYOK providers instead require their validated provider settings and API credentials.
//
// Runtime verification note: the live path spawns the shared Copilot CLI subprocess and streams from
// it. Copilot sessions are gated off when no token is present; API-provider sessions are gated by their
// own configuration. Tests cover availability, provider isolation, prompt building, and confirm routing;
// live hosted-provider inference remains an external integration check.
package webchat

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
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
	// errCopilotTokenRequired keeps the server-only Copilot prerequisite statically matchable.
	errCopilotTokenRequired = errors.New("GitHub Copilot token is required for web chat")
)

// Runner powers the web UI's AI assistant. The shared runtime client is started lazily on the first
// turn and reused across turns (starting it spawns a subprocess, which is too slow per-turn);
// Close stops it. A fresh session is created per turn, so prior turns are replayed via the prompt.
//
// Write tools are gated by per-action confirmation: pending tool-confirm prompts are tracked in the
// confirms registry, keyed by a unique confirmId, so a later ConfirmTool call (driven by the SPA's
// Approve/Deny) can route the decision back to the blocked permission handler.
type Runner struct {
	rootCmd *cobra.Command

	// sessionDefaults supplies provider settings for new sessions, read fresh per turn so Settings
	// changes take effect without a restart. Nil preserves the GitHub Copilot default.
	sessionDefaults func() v1alpha1.ChatSpec

	mu        sync.Mutex
	client    *copilot.Client
	clientKey string

	// confirmMu guards confirms. It is separate from mu (which guards the long-lived client) so a
	// confirm decision never contends with client startup.
	confirmMu sync.Mutex
	// confirms maps a pending confirmId to the channel its blocked permission handler waits on. An entry
	// exists only while a write tool awaits a decision; ConfirmTool sends on and removes it.
	confirms map[string]chan bool
}

// Option configures a Runner.
type Option func(*Runner)

// WithSessionDefaults supplies the provider settings applied to new chat sessions. The
// provider is read fresh per turn, so Settings changes take effect without restarting the server.
func WithSessionDefaults(provider func() v1alpha1.ChatSpec) Option {
	return func(runner *Runner) { runner.sessionDefaults = provider }
}

// New builds a Runner that generates its tools from rootCmd's command tree (the same source the CLI
// chat uses), so the assistant can run the same read operations.
func New(rootCmd *cobra.Command, opts ...Option) *Runner {
	runner := &Runner{rootCmd: rootCmd, confirms: make(map[string]chan bool)}
	for _, opt := range opts {
		opt(runner)
	}

	return runner
}

// Available reports whether the current provider configuration is complete. Copilot requires its
// explicit server token; BYOK providers use their own API-key and endpoint requirements.
func (r *Runner) Available(_ context.Context) bool {
	provider, err := chatsvc.ResolveProvider(r.currentSpec(), os.Getenv)
	if err != nil {
		return false
	}

	return !provider.UsesCopilot() || copilotToken() != ""
}

// Close stops the shared runtime client (and its subprocess) if one was started.
func (r *Runner) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.client != nil {
		_ = r.client.Stop()
		r.client = nil
		r.clientKey = ""
	}
}

// Run runs one chat turn for req, streaming the assistant's reply to emit and ending with a done event
// on normal completion. Read tools are auto-approved; write tools are gated behind a per-action
// confirmation streamed to emit and resolved by ConfirmTool.
func (r *Runner) Run(ctx context.Context, req api.ChatRequest, emit func(api.ChatEvent)) error {
	spec := r.currentSpec()

	provider, err := chatsvc.ResolveProvider(spec, os.Getenv)
	if err != nil {
		return fmt.Errorf("resolve AI provider: %w", err)
	}

	if provider.UsesCopilot() && copilotToken() == "" {
		return errCopilotTokenRequired
	}

	client, err := r.ensureClient(ctx, provider)
	if err != nil {
		return err
	}

	tools, _ := chatsvc.GetKSailToolMetadata(r.rootCmd, nil, toolgen.NewSessionLogRef())
	streaming := true

	sessionConfig := buildSessionConfig(
		provider,
		spec.ReasoningEffort,
		chatsvc.BuildSystemSectionsForProvider(r.rootCmd, provider),
		r.confirmPermissionHandler(ctx, emit),
	)
	sessionConfig.Streaming = &streaming
	sessionConfig.Tools = tools

	session, err := client.CreateSession(ctx, sessionConfig)
	if err != nil {
		return fmt.Errorf("create chat session: %w", err)
	}

	defer func() { _ = session.Disconnect() }()

	return streamTurn(ctx, session, buildPrompt(req), emit)
}

// ConfirmTool resolves a pending write-tool confirmation: it routes approved to the blocked permission
// handler waiting on confirmID. An unknown confirmId (already resolved, timed out, or never issued) is
// a no-op, so a duplicate or late decision is harmless.
func (r *Runner) ConfirmTool(confirmID string, approved bool) {
	r.confirmMu.Lock()
	decision, ok := r.confirms[confirmID]
	r.confirmMu.Unlock()

	if !ok {
		return
	}

	// Non-blocking: the channel is buffered (capacity 1) and read at most once, so a duplicate decision
	// for the same confirmId cannot block here.
	select {
	case decision <- approved:
	default:
	}
}

func (r *Runner) currentSpec() v1alpha1.ChatSpec {
	if r.sessionDefaults == nil {
		return v1alpha1.ChatSpec{}
	}

	return r.sessionDefaults()
}

func buildSessionConfig(
	provider chatsvc.ResolvedProvider,
	reasoningEffort string,
	sections map[string]copilot.SectionOverride,
	permissionHandler copilot.PermissionHandlerFunc,
) *copilot.SessionConfig {
	return &copilot.SessionConfig{
		Model:           provider.Model,
		ReasoningEffort: reasoningEffort,
		Provider:        provider.SDK,
		SystemMessage: &copilot.SystemMessageConfig{
			Mode:     "customize",
			Sections: sections,
		},
		OnPermissionRequest: permissionHandler,
	}
}

// ensureClient starts the shared runtime once per authentication mode and returns it. Switching
// between Copilot and BYOK restarts the runtime so a BYOK process never inherits Copilot login state.
func (r *Runner) ensureClient(
	ctx context.Context,
	provider chatsvc.ResolvedProvider,
) (*copilot.Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	options, key := clientOptions(provider)
	if r.client != nil && r.clientKey == key {
		return r.client, nil
	}

	if r.client != nil {
		_ = r.client.Stop()
		r.client = nil
		r.clientKey = ""
	}

	client := copilot.NewClient(options)

	err := client.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("start chat runtime: %w", err)
	}

	r.client = client
	r.clientKey = key

	return client, nil
}

func clientOptions(provider chatsvc.ResolvedProvider) (*copilot.ClientOptions, string) {
	options := &copilot.ClientOptions{
		LogLevel:                  "error",
		SessionIdleTimeoutSeconds: sessionIdleTimeoutSeconds,
	}

	if provider.UsesCopilot() {
		token := copilotToken()
		options.GitHubToken = token
		options.UseLoggedInUser = new(false)

		return options, "copilot:" + token
	}

	options.UseLoggedInUser = new(false)
	options.Env = filterRuntimeEnv(os.Environ())

	return options, "byok"
}

func filterRuntimeEnv(environment []string) []string {
	blocked := map[string]struct{}{
		"GITHUB_TOKEN": {}, "GH_TOKEN": {}, "COPILOT_GITHUB_TOKEN": {},
		"KSAIL_COPILOT_TOKEN": {}, "COPILOT_TOKEN": {}, "COPILOT_SDK_AUTH_TOKEN": {},
	}

	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, found := strings.Cut(entry, "=")
		if _, excluded := blocked[strings.ToUpper(name)]; found && excluded {
			continue
		}

		filtered = append(filtered, entry)
	}

	return filtered
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

	// Surface a session error instead of a normal completion; emit done only on a clean turn so the
	// client never sees a done frame immediately followed by an error frame.
	select {
	case turnErr := <-errCh:
		return turnErr
	default:
		emit(api.ChatEvent{Type: api.ChatEventDone})

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

// confirmPermissionHandler auto-approves read operations and gates writes behind an explicit
// per-action confirmation: for a write tool it emits a tool-confirm event (a unique confirmId plus the
// tool name and a short args summary) and blocks until the SPA posts a decision (ConfirmTool) or ctx is
// cancelled, then approves or rejects accordingly. This lets the web assistant perform writes only with
// the user's per-action consent, unlike the CLI's interactive TUI.
func (r *Runner) confirmPermissionHandler(
	ctx context.Context, emit func(api.ChatEvent),
) copilot.PermissionHandlerFunc {
	return func(
		request copilot.PermissionRequest, _ copilot.PermissionInvocation,
	) (rpc.PermissionDecision, error) {
		if chatsvc.IsReadOperation(request.Kind()) {
			return &rpc.PermissionDecisionApproveOnce{}, nil
		}

		approved := r.awaitConfirmation(ctx, request, emit)
		if approved {
			return &rpc.PermissionDecisionApproveOnce{}, nil
		}

		return &rpc.PermissionDecisionReject{}, nil
	}
}

// awaitConfirmation emits a tool-confirm event for a write request and blocks until the matching
// ConfirmTool decision arrives or ctx is cancelled (treated as a denial). It always cleans up the
// pending entry so a cancelled turn never leaks a channel.
func (r *Runner) awaitConfirmation(
	ctx context.Context, request copilot.PermissionRequest, emit func(api.ChatEvent),
) bool {
	confirmID := newConfirmID()
	decision := r.registerConfirm(confirmID)

	defer r.clearConfirm(confirmID)

	emit(api.ChatEvent{
		Type:      api.ChatEventToolConfirm,
		ConfirmID: confirmID,
		Text:      writeToolName(request),
		Summary:   writeToolSummary(request),
	})

	select {
	case approved := <-decision:
		return approved
	case <-ctx.Done():
		return false
	}
}

// registerConfirm allocates and stores the decision channel for confirmID, returning it for the
// handler to wait on. The channel is buffered so ConfirmTool never blocks on a slow reader.
func (r *Runner) registerConfirm(confirmID string) chan bool {
	decision := make(chan bool, 1)

	r.confirmMu.Lock()
	r.confirms[confirmID] = decision
	r.confirmMu.Unlock()

	return decision
}

// clearConfirm removes the pending entry for confirmID (idempotent).
func (r *Runner) clearConfirm(confirmID string) {
	r.confirmMu.Lock()
	delete(r.confirms, confirmID)
	r.confirmMu.Unlock()
}

// newConfirmID returns a unique, unguessable id for one pending confirmation, so a decision can only
// resolve the request it was issued for.
func newConfirmID() string {
	return rand.Text()
}

// writeToolName extracts a human-readable tool name from a write permission request. The KSail CLI
// tools surface as custom tools; other write kinds fall back to a generic label so the user always
// sees what is being confirmed.
func writeToolName(request copilot.PermissionRequest) string {
	if tool, ok := request.(rpc.PermissionRequestCustomTool); ok {
		return tool.ToolName
	}

	return string(request.Kind())
}

// writeToolSummary builds a short, display-only summary of what the write tool will do. For a custom
// tool it prefers the tool's own description; it never serializes raw args (they may be large or carry
// secrets), so the summary stays a safe one-liner.
func writeToolSummary(request copilot.PermissionRequest) string {
	if tool, ok := request.(rpc.PermissionRequestCustomTool); ok {
		return tool.ToolDescription
	}

	return ""
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
