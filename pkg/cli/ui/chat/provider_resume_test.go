package chat //nolint:testpackage // Resume construction is a private session-integrity seam.

import (
	"testing"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/stretchr/testify/assert"
)

func TestBuildResumeSessionConfigPreservesBYOKProvider(t *testing.T) {
	t.Parallel()

	provider := &copilot.ProviderConfig{
		Type: "openai", BaseURL: "http://localhost:11434/v1", ModelID: "qwen3:8b",
	}
	sessionConfig := &copilot.SessionConfig{
		Model: "qwen3:8b", Provider: provider, ReasoningEffort: "high",
	}

	resume := buildResumeSessionConfig(sessionConfig)
	assert.Same(t, provider, resume.Provider)
	assert.Equal(t, "high", resume.ReasoningEffort)
}
