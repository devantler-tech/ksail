package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// Server configuration constants.
const (
	serverKeepAlive = 30 * time.Second
	serverPageSize  = 100
)

// ServerConfig contains configuration for the MCP server.
type ServerConfig struct {
	// Name is the server name.
	Name string
	// Version is the server version.
	Version string
	// RootCmd is the root Cobra command for tool generation.
	RootCmd *cobra.Command
	// Logger is the logger for the server (optional).
	Logger *slog.Logger
	// WorkingDirectory is the directory to run commands in (defaults to current directory).
	WorkingDirectory string
	// ExecutablePath is the executable used to run tool commands.
	// DefaultConfig resolves it to the running binary so tool calls work even
	// when the MCP client launches the server with a minimal PATH. When empty,
	// tool execution falls back to resolving the root command name via PATH.
	ExecutablePath string
}

// DefaultConfig returns a default server configuration.
func DefaultConfig(rootCmd *cobra.Command, version string) ServerConfig {
	return ServerConfig{
		Name:             "ksail-mcp",
		Version:          version,
		RootCmd:          rootCmd,
		Logger:           slog.New(slog.NewTextHandler(os.Stderr, nil)),
		WorkingDirectory: "",
		ExecutablePath:   toolgen.DefaultExecutablePath(),
	}
}

// NewServer creates and configures a new MCP server with KSail tools.
func NewServer(cfg ServerConfig) (*mcpsdk.Server, error) {
	// Create server instance
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    cfg.Name,
		Version: cfg.Version,
	}, &mcpsdk.ServerOptions{
		Instructions: "KSail — SDK for managing local Kubernetes clusters. Only Docker is required.\n\n" +
			"Workflow: init → create → [update|info|diagnose] → delete.\n" +
			"Tools: cluster_read (inspect), cluster_write (lifecycle), " +
			"workload_read (get/describe/logs), " +
			"workload_write (apply/create/scale/rollout, plus cipher SOPS secret encryption), " +
			"tenant_write (multi-tenancy).\n" +
			"Cluster lifecycle tools use --name and optionally --kubeconfig to target a cluster. " +
			"Workload tools accept --context to target a kubeconfig context and --namespace for namespace scoping.",
		Logger:    cfg.Logger,
		KeepAlive: serverKeepAlive,
		PageSize:  serverPageSize,
	})

	// Generate tool definitions
	// Use DefaultOptions() to expose all tools except metadata commands.
	// Excluded commands: chat, mcp, completion, help (see toolgen.DefaultOptions()).
	// MCP clients can toggle individual tools on/off as needed.
	opts := toolgen.DefaultOptions()
	if cfg.WorkingDirectory != "" {
		opts.WorkingDirectory = cfg.WorkingDirectory
	}
	// Run tools via the configured executable (DefaultConfig resolves the
	// running binary) instead of relying on a PATH lookup of "ksail".
	opts.ExecutablePath = cfg.ExecutablePath
	// Pass logger for debug logging during command execution
	opts.Logger = cfg.Logger

	toolDefs := toolgen.GenerateTools(cfg.RootCmd, opts)

	// Register tools with MCP server
	toolgen.ToMCPTools(server, toolDefs, opts)

	return server, nil
}

// RunServer creates and runs an MCP server over stdio.
// This is the main entry point for the MCP server.
func RunServer(ctx context.Context, cfg ServerConfig) error {
	// Create server
	server, err := NewServer(cfg)
	if err != nil {
		return fmt.Errorf("creating server: %w", err)
	}

	// Create stdio transport
	transport := &mcpsdk.StdioTransport{}

	// Run server - blocks until client disconnects or context is cancelled
	err = server.Run(ctx, transport)
	if err != nil {
		return fmt.Errorf("running server: %w", err)
	}

	return nil
}
