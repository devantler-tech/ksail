package chat_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/chat"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateElicitationHandler_AcceptSimple(t *testing.T) {
	t.Parallel()

	reader := strings.NewReader("y\n")
	writer := &bytes.Buffer{}
	handler := chat.CreateElicitationHandler(reader, writer)

	result, err := handler(copilot.ElicitationContext{
		Message: "Do you want to continue?",
	})

	require.NoError(t, err)
	assert.Equal(t, copilot.ElicitationActionAccept, result.Action)
	assert.Contains(t, writer.String(), "Input Requested")
	assert.Contains(t, writer.String(), "Do you want to continue?")
}

func TestCreateElicitationHandler_DeclineSimple(t *testing.T) {
	t.Parallel()

	reader := strings.NewReader("n\n")
	writer := &bytes.Buffer{}
	handler := chat.CreateElicitationHandler(reader, writer)

	result, err := handler(copilot.ElicitationContext{
		Message: "Do you want to continue?",
	})

	require.NoError(t, err)
	assert.Equal(t, copilot.ElicitationActionDecline, result.Action)
}

func TestCreateElicitationHandler_DefaultDecline(t *testing.T) {
	t.Parallel()

	reader := strings.NewReader("\n")
	writer := &bytes.Buffer{}
	handler := chat.CreateElicitationHandler(reader, writer)

	result, err := handler(copilot.ElicitationContext{
		Message: "Accept something",
	})

	require.NoError(t, err)
	assert.Equal(t, copilot.ElicitationActionDecline, result.Action)
}

func TestCreateElicitationHandler_EOF(t *testing.T) {
	t.Parallel()

	reader := strings.NewReader("") // EOF immediately
	writer := &bytes.Buffer{}
	handler := chat.CreateElicitationHandler(reader, writer)

	result, err := handler(copilot.ElicitationContext{
		Message: "test",
	})

	require.NoError(t, err)
	assert.Equal(t, copilot.ElicitationActionCancel, result.Action)
}

func TestCreateElicitationHandler_FormFields(t *testing.T) {
	t.Parallel()

	input := "hello\nworld\n"
	reader := strings.NewReader(input)
	writer := &bytes.Buffer{}
	handler := chat.CreateElicitationHandler(reader, writer)

	result, err := handler(copilot.ElicitationContext{
		Mode:    new(copilot.ElicitationRequestedModeForm),
		Message: "Fill in the fields",
		RequestedSchema: &copilot.ElicitationSchema{
			Properties: map[string]any{
				"name":  map[string]any{"type": "string"},
				"value": map[string]any{"type": "string"},
			},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, copilot.ElicitationActionAccept, result.Action)
	// Fields are sorted alphabetically: name, value
	assert.Equal(t, "hello", result.Content["name"])
	assert.Equal(t, "world", result.Content["value"])
}

func TestCreateElicitationHandler_FormFieldDecline(t *testing.T) {
	t.Parallel()

	input := "hello\n!cancel\n"
	reader := strings.NewReader(input)
	writer := &bytes.Buffer{}
	handler := chat.CreateElicitationHandler(reader, writer)

	result, err := handler(copilot.ElicitationContext{
		Mode:    new(copilot.ElicitationRequestedModeForm),
		Message: "Fill in the fields",
		RequestedSchema: &copilot.ElicitationSchema{
			Properties: map[string]any{
				"alpha": map[string]any{"type": "string"},
				"beta":  map[string]any{"type": "string"},
			},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, copilot.ElicitationActionDecline, result.Action)
}

func TestCreateElicitationHandler_FormFieldEOF(t *testing.T) {
	t.Parallel()

	input := "hello\n" // only one field, then EOF
	reader := strings.NewReader(input)
	writer := &bytes.Buffer{}
	handler := chat.CreateElicitationHandler(reader, writer)

	result, err := handler(copilot.ElicitationContext{
		Mode:    new(copilot.ElicitationRequestedModeForm),
		Message: "Fill in the fields",
		RequestedSchema: &copilot.ElicitationSchema{
			Properties: map[string]any{
				"alpha": map[string]any{"type": "string"},
				"beta":  map[string]any{"type": "string"},
			},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, copilot.ElicitationActionCancel, result.Action)
}

func TestCreateElicitationHandler_URLMode(t *testing.T) {
	t.Parallel()

	reader := strings.NewReader("y\n")
	writer := &bytes.Buffer{}
	handler := chat.CreateElicitationHandler(reader, writer)

	result, err := handler(copilot.ElicitationContext{
		Mode:    new(copilot.ElicitationRequestedModeURL),
		URL:     new("https://example.com/auth"),
		Message: "Open this URL to authenticate",
	})

	require.NoError(t, err)
	assert.Equal(t, copilot.ElicitationActionAccept, result.Action)
	assert.Contains(t, writer.String(), "https://example.com/auth")
}

func TestCreateElicitationHandler_EmptySchema(t *testing.T) {
	t.Parallel()

	reader := strings.NewReader("y\n")
	writer := &bytes.Buffer{}
	handler := chat.CreateElicitationHandler(reader, writer)

	result, err := handler(copilot.ElicitationContext{
		Mode:            new(copilot.ElicitationRequestedModeForm),
		Message:         "No fields",
		RequestedSchema: &copilot.ElicitationSchema{},
	})

	require.NoError(t, err)
	assert.Equal(t, copilot.ElicitationActionAccept, result.Action)
}
