package webchat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/github/copilot-sdk/go/rpc"
)

func TestAvailableRequiresToken(t *testing.T) {
	// Not parallel: mutates process env via t.Setenv (restored at test end).
	t.Setenv("KSAIL_COPILOT_TOKEN", "")
	t.Setenv("COPILOT_TOKEN", "")

	runner := &Runner{}
	if runner.Available(context.Background()) {
		t.Error("Available = true with no Copilot token, want false")
	}

	t.Setenv("COPILOT_TOKEN", "a-token")

	if !runner.Available(context.Background()) {
		t.Error("Available = false with COPILOT_TOKEN set, want true")
	}
}

func TestBuildPromptIncludesContextHistoryAndMessage(t *testing.T) {
	t.Parallel()

	got := buildPrompt(api.ChatRequest{
		Cluster:   "kind",
		Namespace: "default",
		History: []api.ChatMessage{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
		},
		Message: "what pods are failing?",
	})

	for _, want := range []string{
		"Active cluster: kind (namespace default).",
		"user: hi",
		"assistant: hello",
		"what pods are failing?",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q:\n%s", want, got)
		}
	}
}

func TestBuildPromptWithoutClusterIsJustMessage(t *testing.T) {
	t.Parallel()

	got := buildPrompt(api.ChatRequest{Message: "hi"})
	if got != "hi" {
		t.Errorf("prompt = %q, want %q", got, "hi")
	}
}

// TestConfirmToolRoutesApproval pins the write-confirm seam without the (token-gated) Copilot path: a
// blocked confirmation resolves with the decision a later ConfirmTool delivers for the same confirmId.
func TestConfirmToolRoutesApproval(t *testing.T) {
	t.Parallel()

	runner := New(nil)

	confirmID := newConfirmID()
	decision := runner.registerConfirm(confirmID)

	go runner.ConfirmTool(confirmID, true)

	select {
	case approved := <-decision:
		if !approved {
			t.Error("ConfirmTool routed approved=false, want true")
		}
	case <-time.After(time.Second):
		t.Fatal("ConfirmTool decision was not delivered")
	}
}

// TestConfirmToolUnknownIDIsNoOp pins that a decision for an unissued/already-resolved confirmId is
// harmless (no panic, no block), so a stale or duplicate SPA decision cannot wedge the runner.
func TestConfirmToolUnknownIDIsNoOp(t *testing.T) {
	t.Parallel()

	runner := New(nil)

	runner.ConfirmTool("never-issued", true)
}

// TestAwaitConfirmationEmitsAndApproves pins the happy path: awaitConfirmation emits a tool-confirm
// event carrying the tool name and summary, and returns true once ConfirmTool approves that confirmId.
func TestAwaitConfirmationEmitsAndApproves(t *testing.T) {
	t.Parallel()

	runner := New(nil)
	request := rpc.PermissionRequestCustomTool{
		ToolName:        "cluster_write",
		ToolDescription: "Create a cluster",
	}

	events := make(chan api.ChatEvent, 1)
	emit := func(event api.ChatEvent) { events <- event }

	result := make(chan bool, 1)
	go func() {
		result <- runner.awaitConfirmation(context.Background(), request, emit)
	}()

	event := <-events
	if event.Type != api.ChatEventToolConfirm {
		t.Fatalf("emitted event type = %q, want tool-confirm", event.Type)
	}

	if event.Text != "cluster_write" || event.Summary != "Create a cluster" {
		t.Errorf(
			"event name/summary = %q/%q, want cluster_write/Create a cluster",
			event.Text,
			event.Summary,
		)
	}

	if event.ConfirmID == "" {
		t.Fatal("emitted event has no confirmId")
	}

	runner.ConfirmTool(event.ConfirmID, true)

	select {
	case approved := <-result:
		if !approved {
			t.Error("awaitConfirmation returned false after approval, want true")
		}
	case <-time.After(time.Second):
		t.Fatal("awaitConfirmation did not return after ConfirmTool")
	}
}

// TestAwaitConfirmationContextCancelDenies pins that a cancelled turn denies a pending write (rather
// than blocking forever) and cleans up the pending entry.
func TestAwaitConfirmationContextCancelDenies(t *testing.T) {
	t.Parallel()

	runner := New(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	request := rpc.PermissionRequestCustomTool{ToolName: "cluster_write"}

	approved := runner.awaitConfirmation(ctx, request, func(api.ChatEvent) {})
	if approved {
		t.Error("awaitConfirmation approved a write on a cancelled context, want denied")
	}

	runner.confirmMu.Lock()
	remaining := len(runner.confirms)
	runner.confirmMu.Unlock()

	if remaining != 0 {
		t.Errorf("pending confirms = %d after cancel, want 0", remaining)
	}
}
