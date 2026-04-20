package toolgen_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errCommandFailed = errors.New("command failed: exit status 1")
	errMissingParam  = errors.New(
		"building command args: missing or invalid subcommand parameter: cluster_operation",
	)
)

func TestToMCPTools_EmptyInput(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(
		&mcp.Implementation{Name: "test", Version: "0.0.1"},
		nil,
	)

	// Should not panic with empty tools
	toolgen.ToMCPTools(server, nil, toolgen.ToolOptions{})

	// Verify server was created (no panic)
	require.NotNil(t, server)
}

func TestToMCPTools_SingleTool(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(
		&mcp.Implementation{Name: "test", Version: "0.0.1"},
		nil,
	)

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

	// Should not panic
	toolgen.ToMCPTools(server, tools, toolgen.ToolOptions{})

	assert.NotNil(t, server)
}

func TestToMCPTools_MultipleTools(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(
		&mcp.Implementation{Name: "test", Version: "0.0.1"},
		nil,
	)

	tools := []toolgen.ToolDefinition{
		{
			Name:        "cluster_create",
			Description: "Create a cluster",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			CommandPath:  "ksail cluster create",
			CommandParts: []string{"ksail", "cluster", "create"},
		},
		{
			Name:        "cluster_delete",
			Description: "Delete a cluster",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			CommandPath:  "ksail cluster delete",
			CommandParts: []string{"ksail", "cluster", "delete"},
		},
		{
			Name:        "workload_get",
			Description: "Get workloads",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			CommandPath:  "ksail workload get",
			CommandParts: []string{"ksail", "workload", "get"},
		},
	}

	// Should not panic with multiple tools
	toolgen.ToMCPTools(server, tools, toolgen.ToolOptions{})

	assert.NotNil(t, server)
}

func TestToMCPTools_DuplicateNameOverwrites(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(
		&mcp.Implementation{Name: "test", Version: "0.0.1"},
		nil,
	)

	tools := []toolgen.ToolDefinition{
		{
			Name:        "same_name",
			Description: "First tool",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			CommandPath:  "ksail first",
			CommandParts: []string{"ksail", "first"},
		},
		{
			Name:        "same_name",
			Description: "Duplicate tool",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			CommandPath:  "ksail second",
			CommandParts: []string{"ksail", "second"},
		},
	}

	// MCP SDK replaces tools with the same name (no panic)
	assert.NotPanics(t, func() {
		toolgen.ToMCPTools(server, tools, toolgen.ToolOptions{})
	})
}

func TestBuildMCPSuccessText(t *testing.T) {
	t.Parallel()

	t.Run("with output", func(t *testing.T) {
		t.Parallel()

		text := toolgen.BuildMCPSuccessText("ksail cluster info", "cluster is running")

		var response map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &response))

		assert.Equal(t, "success", response["status"])
		assert.Equal(t, "ksail cluster info", response["command"])
		assert.Equal(t, "cluster is running", response["output"])
		assert.Nil(t, response["error"])
	})

	t.Run("without output", func(t *testing.T) {
		t.Parallel()

		text := toolgen.BuildMCPSuccessText("ksail cluster create", "")

		var response map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &response))

		assert.Equal(t, "success", response["status"])
		assert.Equal(t, "ksail cluster create", response["command"])
		assert.Nil(t, response["output"])
		assert.Nil(t, response["error"])
	})
}

func TestBuildMCPErrorText(t *testing.T) {
	t.Parallel()

	t.Run("with output", func(t *testing.T) {
		t.Parallel()

		text := toolgen.BuildMCPErrorText(
			"ksail cluster create",
			"some partial output",
			errCommandFailed,
		)

		var response map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &response))

		assert.Equal(t, "error", response["status"])
		assert.Equal(t, "ksail cluster create", response["command"])
		assert.Equal(t, "some partial output", response["output"])
		assert.Equal(t, "command failed: exit status 1", response["error"])
	})

	t.Run("without output", func(t *testing.T) {
		t.Parallel()

		text := toolgen.BuildMCPErrorText(
			"ksail workload get",
			"",
			errMissingParam,
		)

		var response map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &response))

		assert.Equal(t, "error", response["status"])
		assert.Equal(t, "ksail workload get", response["command"])
		assert.Nil(t, response["output"])
		assert.Contains(t, response["error"], "missing or invalid subcommand parameter")
	})
}
