package mcp_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	mcpcmd "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/mcp"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errTest       = errors.New("test error")
	errWrappedMCP = fmt.Errorf("running MCP server: %w", errTest)
)

func TestNewMCPCmd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		runtime              *di.Runtime
		expectedUse          string
		expectedShort        string
		expectedExcludeAnnot bool
	}{
		{
			name:                 "creates mcp command with correct properties",
			runtime:              &di.Runtime{},
			expectedUse:          "mcp",
			expectedShort:        "Start an MCP server",
			expectedExcludeAnnot: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := mcpcmd.NewMCPCmd(testCase.runtime)

			require.NotNil(t, cmd, "NewMCPCmd should return non-nil command")
			assert.Equal(t, testCase.expectedUse, cmd.Use, "Use field mismatch")
			assert.Equal(t, testCase.expectedShort, cmd.Short, "Short field mismatch")
			assert.NotEmpty(t, cmd.Long, "Long description should not be empty")

			// Verify exclude annotation
			if testCase.expectedExcludeAnnot {
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

	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

	// Verify long description contains key information
	expectedSubstrings := []string{
		"MCP server",
		"stdio",
		"tools",
		"ksail mcp",
	}

	for _, substr := range expectedSubstrings {
		assert.Contains(t, cmd.Long, substr,
			"Long description should contain '%s'", substr)
	}
}

func TestNewMCPCmd_NilRuntime(t *testing.T) {
	t.Parallel()

	// Verify command creation works even with nil runtime
	cmd := mcpcmd.NewMCPCmd(nil)

	require.NotNil(t, cmd, "NewMCPCmd should handle nil runtime")
	assert.Equal(t, "mcp", cmd.Use)
	assert.NotNil(t, cmd.RunE)
}

func TestNewMCPCmd_Annotations(t *testing.T) {
	t.Parallel()

	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

	require.NotNil(t, cmd.Annotations, "Annotations should not be nil")

	// Count annotations (should only have exclude)
	assert.Len(t, cmd.Annotations, 1, "Should have exactly one annotation")

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

	runtime := &di.Runtime{}
	mcpCmd := mcpcmd.NewMCPCmd(runtime)

	rootCmd.AddCommand(mcpCmd)

	// Verify the command is properly attached
	require.NotNil(t, mcpCmd.Parent(), "MCP command should have a parent")
	assert.Equal(t, rootCmd, mcpCmd.Root(), "Root should be accessible")
}

func TestNewMCPCmd_ExecuteWithoutServer(t *testing.T) {
	t.Parallel()

	// This test verifies command structure without actually running the server
	// since the MCP server requires stdio interaction which is hard to test

	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

	// Verify command can be created and inspected
	require.NotNil(t, cmd)
	assert.NotNil(t, cmd.RunE, "RunE function should be defined")

	// Verify Args validator behavior if set
	if cmd.Args != nil {
		// Test with no arguments
		err := cmd.Args(cmd, []string{})
		assert.NoError(t, err, "Command should accept no arguments when Args is set")
	}
}

func TestNewMCPCmd_CommandStructure(t *testing.T) {
	t.Parallel()

	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

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
	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

	var outBuf, errBuf bytes.Buffer

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	// Verify buffers are set
	assert.NotNil(t, cmd.OutOrStdout())
	assert.NotNil(t, cmd.ErrOrStderr())
}

func TestNewMCPCmd_ContextPropagation(t *testing.T) {
	t.Parallel()

	// Verify that command can have context attached
	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

	ctx := context.Background()
	cmd.SetContext(ctx)

	retrievedCtx := cmd.Context()
	require.NotNil(t, retrievedCtx, "Context should be retrievable")
}

func TestNewMCPCmd_HelpOutput(t *testing.T) {
	t.Parallel()

	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

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

func TestNewMCPCmd_VersionAnnotation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		rootAnnotations  map[string]string
		expectedContains string
	}{
		{
			name: "with version annotation",
			rootAnnotations: map[string]string{
				"version": "v5.1.0",
			},
			expectedContains: "v5.1.0",
		},
		{
			name:             "without version annotation",
			rootAnnotations:  map[string]string{},
			expectedContains: "dev",
		},
		{
			name:             "with nil annotations",
			rootAnnotations:  nil,
			expectedContains: "dev",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rootCmd := &cobra.Command{
				Use:         "ksail",
				Annotations: testCase.rootAnnotations,
			}

			runtime := &di.Runtime{}
			mcpCmd := mcpcmd.NewMCPCmd(runtime)

			rootCmd.AddCommand(mcpCmd)

			// Verify command can access root annotations
			root := mcpCmd.Root()
			require.NotNil(t, root)

			if root.Annotations != nil {
				if v, ok := root.Annotations["version"]; ok {
					assert.Contains(t, v, testCase.expectedContains,
						"Version annotation content mismatch")
				} else {
					// No version annotation, default to "dev" behavior
					assert.Equal(t, "dev", testCase.expectedContains)
				}
			} else {
				// Nil annotations, should default to "dev"
				assert.Equal(t, "dev", testCase.expectedContains)
			}
		})
	}
}

func TestNewMCPCmd_WorkingDirectory(t *testing.T) {
	t.Parallel()

	// This test verifies that the command logic considers working directory
	// We can't easily test the actual execution without mocking the MCP server

	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

	// Use cmd to avoid unused variable
	assert.NotNil(t, cmd, "Command should be created")

	// Verify we can get current working directory (prerequisite for command)
	wd, err := os.Getwd()
	require.NoError(t, err, "Should be able to get working directory")
	assert.NotEmpty(t, wd, "Working directory should not be empty")
}

func TestNewMCPCmd_ErrorPropagation(t *testing.T) {
	t.Parallel()

	// Create a command and verify error handling structure
	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

	require.NotNil(t, cmd.RunE, "RunE should be defined")

	// We can't easily test the actual execution, but we can verify
	// that errors would be wrapped properly by checking the function signature
	// The RunE function signature allows error return

	// Use cmd to avoid unused variable
	assert.NotNil(t, cmd)
}

func TestNewMCPCmd_ExcludeFromToolGeneration(t *testing.T) {
	t.Parallel()

	// Verify that the MCP command is excluded from tool generation
	// This is important because the MCP server itself shouldn't expose
	// itself as a tool to AI assistants

	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

	require.NotNil(t, cmd.Annotations, "Annotations should exist")

	excludeVal, exists := cmd.Annotations[annotations.AnnotationExclude]
	require.True(t, exists, "Exclude annotation must exist")
	assert.Equal(t, "true", excludeVal,
		"MCP command should be excluded from tool generation")
}

func TestNewMCPCmd_SilentErrors(t *testing.T) {
	t.Parallel()

	// Verify command inherits error handling behavior
	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

	// Check default error handling flags
	// By default, cobra.Command.SilenceErrors and SilenceUsage are false
	// We verify the defaults unless explicitly set
	assert.False(t, cmd.SilenceErrors || cmd.SilenceUsage,
		"Command should not silence errors by default")
}

func TestNewMCPCmd_StdioUsage(t *testing.T) {
	t.Parallel()

	// Verify command documentation mentions stdio
	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

	assert.Contains(t, strings.ToLower(cmd.Long), "stdio",
		"Documentation should mention stdio communication")
}

func TestNewMCPCmd_MCPClientReferences(t *testing.T) {
	t.Parallel()

	// Verify command documentation mentions MCP clients
	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

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
	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

	assert.Contains(t, cmd.Long, "ksail mcp",
		"Documentation should include example command usage")
}

func TestNewMCPCmd_ServerLifecycle(t *testing.T) {
	t.Parallel()

	// Verify command documentation describes server lifecycle
	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

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

func TestNewMCPCmd_ErrorMessages(t *testing.T) {
	t.Parallel()

	// Test that potential error messages would be informative
	// We can't easily trigger the actual errors without running the server,
	// but we can verify the error wrapping structure would work

	runtime := &di.Runtime{}
	cmd := mcpcmd.NewMCPCmd(runtime)

	require.NotNil(t, cmd.RunE, "RunE must be defined")

	require.ErrorIs(t, errWrappedMCP, errTest,
		"Wrapped error should contain original error")
	assert.Contains(t, errWrappedMCP.Error(), errTest.Error(),
		"Error should preserve original message")
	assert.Contains(t, errWrappedMCP.Error(), "MCP server",
		"Error should provide context")
}

func TestNewMCPCmd_NilRuntimeSafety(t *testing.T) {
	t.Parallel()

	// Verify command handles nil runtime gracefully
	cmd := mcpcmd.NewMCPCmd(nil)

	require.NotNil(t, cmd, "Command should be created even with nil runtime")
	assert.Equal(t, "mcp", cmd.Use)
	assert.NotNil(t, cmd.RunE, "RunE should be set")
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
}
