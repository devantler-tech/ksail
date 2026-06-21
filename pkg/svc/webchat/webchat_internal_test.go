package webchat

import (
	"context"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
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
