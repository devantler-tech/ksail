// Package mcp provides an MCP server for exposing KSail commands as tools.
package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/ai/toolgen"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp" //nolint:depguard // MCP SDK is required for MCP server
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
}

// DefaultConfig returns a default server configuration.
func DefaultConfig(rootCmd *cobra.Command, version string) ServerConfig {
	return ServerConfig{
		Name:             "ksail-mcp",
		Version:          version,
		RootCmd:          rootCmd,
		Logger:           slog.New(slog.NewTextHandler(os.Stderr, nil)),
		WorkingDirectory: "",
	}
}

// NewServer creates and configures a new MCP server with KSail tools.
func NewServer(cfg ServerConfig) (*mcpsdk.Server, error) {
	// Create server instance
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    cfg.Name,
		Version: cfg.Version,
	}, &mcpsdk.ServerOptions{
		Instructions: "KSail MCP server - provides access to KSail Kubernetes cluster management commands",
		Logger:       cfg.Logger,
		KeepAlive:    serverKeepAlive,
		PageSize:     serverPageSize,
	})

	// Generate tool definitions
	// Use DefaultOptions() to expose all tools except metadata commands.
	// Excluded commands: chat, mcp, completion, help (see toolgen.DefaultOptions()).
	// MCP clients can toggle individual tools on/off as needed.
	opts := toolgen.DefaultOptions()
	if cfg.WorkingDirectory != "" {
		opts.WorkingDirectory = cfg.WorkingDirectory
	}

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
