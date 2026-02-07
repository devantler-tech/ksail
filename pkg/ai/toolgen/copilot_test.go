package toolgen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/ai/toolgen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToCopilotTools_EmptyInput(t *testing.T) {
	t.Parallel()

	result := toolgen.ToCopilotTools(nil, toolgen.ToolOptions{})

	assert.Empty(t, result)
}

func TestToCopilotTools_SingleTool(t *testing.T) {
	t.Parallel()

	tools := []toolgen.ToolDefinition{
		{
			Name:        "cluster_create",
			Description: "Create a new cluster",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Cluster name",
					},
				},
			},
			CommandPath:  "ksail cluster create",
			CommandParts: []string{"ksail", "cluster", "create"},
		},
	}

	result := toolgen.ToCopilotTools(tools, toolgen.ToolOptions{})

	require.Len(t, result, 1)
	assert.Equal(t, "cluster_create", result[0].Name)
	assert.Equal(t, "Create a new cluster", result[0].Description)
	assert.NotNil(t, result[0].Parameters)
	assert.NotNil(t, result[0].Handler, "handler should be set")
}

func TestToCopilotTools_MultipleTools(t *testing.T) {
	t.Parallel()

	tools := []toolgen.ToolDefinition{
		{
			Name:         "cluster_create",
			Description:  "Create a cluster",
			CommandPath:  "ksail cluster create",
			CommandParts: []string{"ksail", "cluster", "create"},
		},
		{
			Name:         "cluster_delete",
			Description:  "Delete a cluster",
			CommandPath:  "ksail cluster delete",
			CommandParts: []string{"ksail", "cluster", "delete"},
		},
		{
			Name:         "cluster_info",
			Description:  "Show cluster info",
			CommandPath:  "ksail cluster info",
			CommandParts: []string{"ksail", "cluster", "info"},
		},
	}

	result := toolgen.ToCopilotTools(tools, toolgen.ToolOptions{})

	require.Len(t, result, 3)

	for i, tool := range tools {
		assert.Equal(t, tool.Name, result[i].Name)
		assert.Equal(t, tool.Description, result[i].Description)
		assert.NotNil(t, result[i].Handler)
	}
}

func TestToCopilotTools_PreservesParameters(t *testing.T) {
	t.Parallel()

	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "The cluster name",
			},
			"workers": map[string]any{
				"type":        "integer",
				"description": "Number of workers",
				"default":     float64(3),
			},
		},
		"required": []any{"name"},
	}

	tools := []toolgen.ToolDefinition{
		{
			Name:         "cluster_create",
			Description:  "Create a cluster",
			Parameters:   params,
			CommandPath:  "ksail cluster create",
			CommandParts: []string{"ksail", "cluster", "create"},
		},
	}

	result := toolgen.ToCopilotTools(tools, toolgen.ToolOptions{})

	require.Len(t, result, 1)
	assert.Equal(t, params, result[0].Parameters)
}

func TestToCopilotTools_NilParameters(t *testing.T) {
	t.Parallel()

	tools := []toolgen.ToolDefinition{
		{
			Name:         "simple_tool",
			Description:  "A tool with no params",
			Parameters:   nil,
			CommandPath:  "ksail simple",
			CommandParts: []string{"ksail", "simple"},
		},
	}

	result := toolgen.ToCopilotTools(tools, toolgen.ToolOptions{})

	require.Len(t, result, 1)
	assert.Nil(t, result[0].Parameters)
	assert.NotNil(t, result[0].Handler)
}
