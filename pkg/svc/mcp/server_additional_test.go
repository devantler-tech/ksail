package mcp_test

import (
	"context"
	"log/slog"
	"runtime"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mcp"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig_Version(t *testing.T) {
	t.Parallel()

	rootCmd := &cobra.Command{Use: "ksail", Short: "KSail CLI"}

	cfg := mcp.DefaultConfig(rootCmd, "2.5.0")

	assert.Equal(t, "2.5.0", cfg.Version)
	assert.Equal(t, "ksail-mcp", cfg.Name)
	assert.Same(t, rootCmd, cfg.RootCmd)
}

func TestDefaultConfig_LoggerNotNil(t *testing.T) {
	t.Parallel()

	rootCmd := &cobra.Command{Use: "ksail"}

	cfg := mcp.DefaultConfig(rootCmd, "1.0.0")

	assert.NotNil(t, cfg.Logger, "default config should have a non-nil logger")
}

func TestDefaultConfig_WorkingDirectoryEmpty(t *testing.T) {
	t.Parallel()

	rootCmd := &cobra.Command{Use: "ksail"}

	cfg := mcp.DefaultConfig(rootCmd, "1.0.0")

	assert.Empty(t, cfg.WorkingDirectory)
}

func TestNewServer_NoSubcommands(t *testing.T) {
	t.Parallel()

	rootCmd := &cobra.Command{
		Use:   "ksail",
		Short: "KSail CLI",
	}

	cfg := mcp.ServerConfig{
		Name:    "test-server",
		Version: "0.1.0",
		RootCmd: rootCmd,
	}

	server, err := mcp.NewServer(cfg)

	require.NoError(t, err)
	assert.NotNil(t, server)
}

func TestNewServer_WithLogger(t *testing.T) {
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

	logger := slog.Default()

	cfg := mcp.ServerConfig{
		Name:    "test-server",
		Version: "0.1.0",
		RootCmd: rootCmd,
		Logger:  logger,
	}

	server, err := mcp.NewServer(cfg)

	require.NoError(t, err)
	assert.NotNil(t, server)
}

func TestNewServer_MultipleCommands(t *testing.T) {
	t.Parallel()

	rootCmd := &cobra.Command{
		Use:   "ksail",
		Short: "KSail CLI",
	}

	for _, name := range []string{"create", "delete", "list", "status"} {
		cmd := &cobra.Command{
			Use:   name,
			Short: name + " command",
			Run:   func(_ *cobra.Command, _ []string) {},
		}
		rootCmd.AddCommand(cmd)
	}

	cfg := mcp.ServerConfig{
		Name:    "test-server",
		Version: "0.1.0",
		RootCmd: rootCmd,
	}

	server, err := mcp.NewServer(cfg)

	require.NoError(t, err)
	assert.NotNil(t, server)
}

func TestNewServer_ListTools_Empty(t *testing.T) {
	t.Parallel()

	// Root with no runnable children -> 0 tools
	rootCmd := &cobra.Command{
		Use:   "ksail",
		Short: "KSail CLI",
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	cfg := mcp.ServerConfig{
		Name:    "test-server",
		Version: "0.1.0",
		RootCmd: rootCmd,
	}

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

	result, err := clientSession.ListTools(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, result.Tools)
}

func TestNewServer_CallTool_WithArgs(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsOS {
		t.Skip("echo is a shell builtin on Windows, not an executable")
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Build a tree where "echo" is root, "hello" is a subcommand that accepts args
	rootCmd := &cobra.Command{
		Use:   "echo",
		Short: "Echo root for testing",
	}
	helloCmd := &cobra.Command{
		Use:   "hello",
		Short: "Print hello",
		Args:  cobra.ArbitraryArgs,
		Run:   func(_ *cobra.Command, _ []string) {},
	}
	rootCmd.AddCommand(helloCmd)

	cfg := mcp.ServerConfig{
		Name:    "test-server",
		Version: "0.1.0",
		RootCmd: rootCmd,
	}

	clientSession := connectClientServer(ctx, t, cfg)

	result, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "hello",
		Arguments: map[string]any{"args": []any{"world"}},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)
}

func TestRunServer_ContextCancelled(t *testing.T) {
	t.Parallel()

	rootCmd := &cobra.Command{
		Use:   "ksail",
		Short: "KSail CLI",
	}

	// RunServer uses StdioTransport which reads from os.Stdin.
	// We can't easily test it without mocking stdio, but we can verify
	// that a cancelled context returns an error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := mcp.RunServer(ctx, mcp.ServerConfig{
		Name:    "test-server",
		Version: "0.1.0",
		RootCmd: rootCmd,
	})

	// RunServer should return an error because the context is already cancelled
	assert.Error(t, err)
}
