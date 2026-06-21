package mcp_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	mcpcmd "github.com/devantler-tech/ksail/v7/pkg/cli/cmd/open/mcp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMCPCmd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		expectedUse          string
		expectedShort        string
		expectedExcludeAnnot bool
	}{
		{
			name:                 "creates mcp command with correct properties",
			expectedUse:          "mcp",
			expectedShort:        "Start an MCP server",
			expectedExcludeAnnot: true,
		},
	}

	for _, testCase := range tests {
		currentTest := testCase
		t.Run(currentTest.name, func(t *testing.T) {
			t.Parallel()

			cmd := mcpcmd.NewMCPCmd()

			require.NotNil(t, cmd, "NewMCPCmd should return non-nil command")
			assert.Equal(t, currentTest.expectedUse, cmd.Use, "Use field mismatch")
			assert.Equal(t, currentTest.expectedShort, cmd.Short, "Short field mismatch")
			assert.NotEmpty(t, cmd.Long, "Long description should not be empty")

			// Verify exclude annotation
			if currentTest.expectedExcludeAnnot {
				require.NotNil(t, cmd.Annotations, "Annotations should not be nil")
				val, ok := cmd.Annotations[annotations.AnnotationExclude]
				assert.True(t, ok, "Exclude annotation should exist")
				assert.Equal(t, "true", val, "Exclude annotation value mismatch")
			}

			// Verify RunE is set
			assert.NotNil(t, cmd.RunE, "RunE should be set")
		})
	}
}

func TestNewMCPCmd_LongDescription(t *testing.T) {
	t.Parallel()

	cmd := mcpcmd.NewMCPCmd()

	// Verify long description contains key information
	expectedSubstrings := []string{
		"MCP server",
		"stdio",
		"tools",
		"ksail open mcp",
	}

	for _, substr := range expectedSubstrings {
		assert.Contains(t, cmd.Long, substr,
			"Long description should contain '%s'", substr)
	}
}

func TestNewMCPCmd_Annotations(t *testing.T) {
	t.Parallel()

	cmd := mcpcmd.NewMCPCmd()

	require.NotNil(t, cmd.Annotations, "Annotations should not be nil")

	// Verify exclude annotation exists and is "true"
	excludeVal, exists := cmd.Annotations[annotations.AnnotationExclude]
	require.True(t, exists, "Exclude annotation must exist")
	assert.Equal(t, "true", excludeVal, "Exclude annotation should be 'true'")
}

func TestNewMCPCmd_RootCommandIntegration(t *testing.T) {
	t.Parallel()

	// Create a root command to simulate real usage
	rootCmd := &cobra.Command{
		Use:     "ksail",
		Version: "v5.0.0",
		Annotations: map[string]string{
			"version": "v5.0.0",
		},
	}

	mcpCmd := mcpcmd.NewMCPCmd()

	rootCmd.AddCommand(mcpCmd)

	// Verify the command is properly attached
	require.NotNil(t, mcpCmd.Parent(), "MCP command should have a parent")
	assert.Equal(t, rootCmd, mcpCmd.Root(), "Root should be accessible")
}

func TestNewMCPCmd_ExecuteWithoutServer(t *testing.T) {
	t.Parallel()

	// This test verifies command structure without actually running the server
	// since the MCP server requires stdio interaction which is hard to test

	cmd := mcpcmd.NewMCPCmd()

	// Verify command can be created and inspected
	require.NotNil(t, cmd)
	assert.NotNil(t, cmd.RunE, "RunE function should be defined")

	// Verify Args validator configuration: MCP command does not define a custom Args validator
	assert.Nil(t, cmd.Args, "MCP command should not define a custom Args validator")
}

func TestNewMCPCmd_CommandStructure(t *testing.T) {
	t.Parallel()

	cmd := mcpcmd.NewMCPCmd()

	// Verify no subcommands (mcp is a leaf command)
	assert.Empty(t, cmd.Commands(), "MCP command should have no subcommands")

	// Verify no flags (mcp command has no flags)
	flagCount := 0

	cmd.Flags().VisitAll(func(*pflag.Flag) {
		flagCount++
	})
	assert.Equal(t, 0, flagCount, "MCP command should have no local flags")
}

func TestNewMCPCmd_OutputBuffer(t *testing.T) {
	t.Parallel()

	// Test that command can use custom output buffers
	cmd := mcpcmd.NewMCPCmd()

	var outBuf, errBuf bytes.Buffer

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	// Verify OutOrStdout/ErrOrStderr return the configured buffers
	assert.Same(t, &outBuf, cmd.OutOrStdout())
	assert.Same(t, &errBuf, cmd.ErrOrStderr())

	// Verify that printing via the command writes into the buffers
	_, err := cmd.OutOrStdout().Write([]byte("stdout test"))
	require.NoError(t, err)
	_, err = cmd.ErrOrStderr().Write([]byte("stderr test"))
	require.NoError(t, err)

	assert.Equal(t, "stdout test", outBuf.String())
	assert.Equal(t, "stderr test", errBuf.String())
}

func TestNewMCPCmd_ContextPropagation(t *testing.T) {
	t.Parallel()

	// Verify that command can have context attached
	cmd := mcpcmd.NewMCPCmd()

	ctx := context.Background()
	cmd.SetContext(ctx)

	retrievedCtx := cmd.Context()
	require.NotNil(t, retrievedCtx, "Context should be retrievable")
	assert.Equal(t, ctx, retrievedCtx,
		"Context returned by command should match the context set via SetContext")
}

func TestNewMCPCmd_HelpOutput(t *testing.T) {
	t.Parallel()

	cmd := mcpcmd.NewMCPCmd()

	// Capture help output
	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err, "Help execution should not error")

	helpOutput := buf.String()

	// Verify help output contains key information
	assert.Contains(t, helpOutput, "mcp", "Help should contain command name")
	assert.Contains(t, helpOutput, "MCP server", "Help should describe MCP server")
}

func TestNewMCPCmd_ExcludeFromToolGeneration(t *testing.T) {
	t.Parallel()

	// Verify that the MCP command is excluded from tool generation
	// This is important because the MCP server itself shouldn't expose
	// itself as a tool to AI assistants

	cmd := mcpcmd.NewMCPCmd()

	require.NotNil(t, cmd.Annotations, "Annotations should exist")

	excludeVal, exists := cmd.Annotations[annotations.AnnotationExclude]
	require.True(t, exists, "Exclude annotation must exist")
	assert.Equal(t, "true", excludeVal,
		"MCP command should be excluded from tool generation")
}

func TestNewMCPCmd_StdioUsage(t *testing.T) {
	t.Parallel()

	// Verify command documentation mentions stdio
	cmd := mcpcmd.NewMCPCmd()

	assert.Contains(t, strings.ToLower(cmd.Long), "stdio",
		"Documentation should mention stdio communication")
}

func TestNewMCPCmd_MCPClientReferences(t *testing.T) {
	t.Parallel()

	// Verify command documentation mentions MCP clients
	cmd := mcpcmd.NewMCPCmd()

	longLower := strings.ToLower(cmd.Long)

	expectedMentions := []string{
		"mcp client",
		"ai assistant",
		"claude",
	}

	found := false

	for _, mention := range expectedMentions {
		if strings.Contains(longLower, mention) {
			found = true

			break
		}
	}

	assert.True(t, found,
		"Documentation should mention MCP clients, AI assistants, or specific tools")
}

func TestNewMCPCmd_ExampleUsage(t *testing.T) {
	t.Parallel()

	// Verify command documentation includes example usage
	cmd := mcpcmd.NewMCPCmd()

	assert.Contains(t, cmd.Long, "ksail open mcp",
		"Documentation should include example command usage")
}

func TestNewMCPCmd_ServerLifecycle(t *testing.T) {
	t.Parallel()

	// Verify command documentation describes server lifecycle
	cmd := mcpcmd.NewMCPCmd()

	longLower := strings.ToLower(cmd.Long)

	assert.Contains(t, longLower, "run",
		"Documentation should mention server runs/starts")

	// Should mention termination or disconnect behavior
	hasTerminationInfo := strings.Contains(longLower, "disconnect") ||
		strings.Contains(longLower, "terminat") ||
		strings.Contains(longLower, "until")

	assert.True(t, hasTerminationInfo,
		"Documentation should describe server lifecycle/termination")
}
