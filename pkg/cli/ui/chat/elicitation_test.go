package chat_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- extractFieldNames tests ---

func TestExtractFieldNames_NilSchema(t *testing.T) {
	t.Parallel()

	fields := chat.ExportExtractFieldNames(nil)
	assert.Nil(t, fields)
}

func TestExtractFieldNames_EmptyProperties(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"properties": map[string]any{},
	}

	fields := chat.ExportExtractFieldNames(schema)
	assert.Nil(t, fields)
}

func TestExtractFieldNames_WithProperties(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"properties": map[string]any{
			"name":  map[string]any{"type": "string"},
			"email": map[string]any{"type": "string"},
		},
	}

	fields := chat.ExportExtractFieldNames(schema)
	assert.Len(t, fields, 2)
	assert.Contains(t, fields, "name")
	assert.Contains(t, fields, "email")
}

func TestExtractFieldNames_NonMapProperties(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"properties": "not a map",
	}

	fields := chat.ExportExtractFieldNames(schema)
	assert.Nil(t, fields)
}

// --- CreateTUIElicitationHandler tests ---

func TestCreateTUIElicitationHandler_SendsRequestAndReturnsResult(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 1)
	handler := chat.CreateTUIElicitationHandler(eventChan)

	// Run handler in a goroutine since it blocks on the response channel
	resultChan := make(chan copilot.ElicitationResult, 1)

	go func() {
		result, err := handler(copilot.ElicitationContext{
			Message:           "Pick a repository",
			ElicitationSource: "github-mcp",
			Mode:              "form",
			RequestedSchema: map[string]any{
				"properties": map[string]any{
					"repo": map[string]any{"type": "string"},
				},
			},
		})
		assert.NoError(t, err)

		resultChan <- result
	}()

	// Read the message from the event channel
	msg := <-eventChan
	req, ok := msg.(chat.ExportElicitationRequestMsg)
	require.True(t, ok, "expected elicitationRequestMsg, got %T", msg)
	assert.Equal(t, "Pick a repository", req.Message)
	assert.Equal(t, "github-mcp", req.Source)
	assert.Equal(t, "form", req.Mode)

	// Send response back through the response channel
	req.Response <- chat.ExportElicitationResponsePayload{
		Result: copilot.ElicitationResult{
			Action:  "accept",
			Content: map[string]any{"repo": "ksail"},
		},
	}

	result := <-resultChan
	assert.Equal(t, "accept", result.Action)
	assert.Equal(t, "ksail", result.Content["repo"])
}

func TestCreateTUIElicitationHandler_DeclineResult(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 1)
	handler := chat.CreateTUIElicitationHandler(eventChan)

	resultChan := make(chan copilot.ElicitationResult, 1)

	go func() {
		result, err := handler(copilot.ElicitationContext{
			Message: "Confirm action",
			Mode:    "form",
		})
		assert.NoError(t, err)

		resultChan <- result
	}()

	msg := <-eventChan
	req, ok := msg.(chat.ExportElicitationRequestMsg)
	require.True(t, ok, "expected elicitationRequestMsg, got %T", msg)

	req.Response <- chat.ExportElicitationResponsePayload{
		Result: copilot.ElicitationResult{Action: "decline"},
	}

	result := <-resultChan
	assert.Equal(t, "decline", result.Action)
	assert.Nil(t, result.Content)
}
