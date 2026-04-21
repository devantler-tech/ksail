package mcp_test

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mcp"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testTimeout = 10 * time.Second
	windowsOS   = "windows"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	rootCmd := &cobra.Command{
		Use:   "ksail",
		Short: "KSail CLI",
	}

	cfg := mcp.DefaultConfig(rootCmd, "1.0.0")

	assert.Equal(t, "ksail-mcp", cfg.Name)
	assert.Equal(t, "1.0.0", cfg.Version)
	assert.Same(t, rootCmd, cfg.RootCmd)
	assert.NotNil(t, cfg.Logger)
	assert.Empty(t, cfg.WorkingDirectory)
}

func TestNewServer(t *testing.T) {
	t.Parallel()

	rootCmd := &cobra.Command{
		Use:   "ksail",
		Short: "KSail CLI",
	}
	subCmd := &cobra.Command{
		Use:   "test",
		Short: "Test command",
		Run:   func(_ *cobra.Command, _ []string) {},
	}
	rootCmd.AddCommand(subCmd)

	cfg := mcp.ServerConfig{
		Name:    "test-server",
		Version: "0.1.0",
		RootCmd: rootCmd,
	}

	server, err := mcp.NewServer(cfg)

	require.NoError(t, err)
	assert.NotNil(t, server)
}

func TestNewServer_WithWorkingDirectory(t *testing.T) {
	t.Parallel()

	rootCmd := &cobra.Command{
		Use:   "ksail",
		Short: "KSail CLI",
	}

	cfg := mcp.ServerConfig{
		Name:             "test-server",
		Version:          "0.1.0",
		RootCmd:          rootCmd,
		WorkingDirectory: "/tmp/test",
	}

	server, err := mcp.NewServer(cfg)

	require.NoError(t, err)
	assert.NotNil(t, server)
}

func TestServerConfig_Fields(t *testing.T) {
	t.Parallel()

	rootCmd := &cobra.Command{Use: "test"}

	cfg := mcp.ServerConfig{
		Name:             "my-server",
		Version:          "2.0.0",
		RootCmd:          rootCmd,
		WorkingDirectory: "/custom/dir",
	}

	assert.Equal(t, "my-server", cfg.Name)
	assert.Equal(t, "2.0.0", cfg.Version)
	assert.Same(t, rootCmd, cfg.RootCmd)
	assert.Equal(t, "/custom/dir", cfg.WorkingDirectory)
}

// newTestCobraTree builds a Cobra command tree rooted at "echo" with leaf
// commands "hello" and "world". Because the root command is "echo", the
// tool executor will run `echo hello` / `echo world` — real shell commands
// that succeed without the ksail binary.
// NOTE: echo is a standalone binary on Unix systems; on Windows it is a
// shell builtin, so tests using this helper are skipped on Windows.
func newTestCobraTree() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "echo",
		Short: "Echo root for testing",
	}
	helloCmd := &cobra.Command{
		Use:   "hello",
		Short: "Print hello",
		Run:   func(_ *cobra.Command, _ []string) {},
	}
	worldCmd := &cobra.Command{
		Use:   "world",
		Short: "Print world",
		Run:   func(_ *cobra.Command, _ []string) {},
	}
	rootCmd.AddCommand(helloCmd, worldCmd)

	return rootCmd
}

// connectClientServer creates an MCP server from cfg, connects it to a new
// MCP client over in-memory transports, and registers t.Cleanup handlers to
// close both sessions automatically (avoiding leaks on partial setup failures).
func connectClientServer(
	ctx context.Context,
	t *testing.T,
	cfg mcp.ServerConfig,
) *mcpsdk.ClientSession {
	t.Helper()

	server, err := mcp.NewServer(cfg)
	require.NoError(t, err)

	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "test-client", Version: "0.0.1"},
		nil,
	)

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)

	t.Cleanup(func() { _ = serverSession.Close() })

	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)

	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession
}

func TestNewServer_ListTools(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsOS {
		t.Skip("echo is a shell builtin on Windows, not an executable")
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	cfg := mcp.ServerConfig{
		Name:    "test-server",
		Version: "0.1.0",
		RootCmd: newTestCobraTree(),
	}

	clientSession := connectClientServer(ctx, t, cfg)

	result, err := clientSession.ListTools(ctx, nil)
	require.NoError(t, err)

	// The tree has two leaf commands: hello, world.
	toolNames := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		toolNames = append(toolNames, tool.Name)
	}

	assert.Contains(t, toolNames, "hello")
	assert.Contains(t, toolNames, "world")
	assert.Len(t, result.Tools, 2)
}

func TestNewServer_CallTool(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsOS {
		t.Skip("echo is a shell builtin on Windows, not an executable")
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	cfg := mcp.ServerConfig{
		Name:    "test-server",
		Version: "0.1.0",
		RootCmd: newTestCobraTree(),
	}

	clientSession := connectClientServer(ctx, t, cfg)

	result, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "hello",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// The executor runs `echo hello` which outputs "hello\n".
	// The handler wraps the output in a structured JSON response.
	assert.False(t, result.IsError, "tool call should succeed")
	require.NotEmpty(t, result.Content)

	textContent, ok := result.Content[0].(*mcpsdk.TextContent)
	require.True(t, ok, "content should be TextContent")

	// Verify response is valid JSON with expected structure
	var response map[string]any
	require.NoError(t, json.Unmarshal([]byte(textContent.Text), &response))
	assert.Equal(t, "success", response["status"])
	assert.Equal(t, "echo hello", response["command"])
	assert.Contains(t, response["output"], "hello")
}
