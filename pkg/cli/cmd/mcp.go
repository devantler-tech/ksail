package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	mcpsvc "github.com/devantler-tech/ksail/v5/pkg/svc/mcp"
	"github.com/spf13/cobra"
)

// NewMCPCmd creates and returns the mcp command.
func NewMCPCmd(_ *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start an MCP server",
		Long: `Start an MCP server that exposes KSail commands as tools.

The MCP server uses stdio for communication and is designed to be
consumed by MCP clients such as AI assistants and automation tools.

Example usage with an MCP client:
  claude desktop or other MCP-compatible client can connect to:
  ksail mcp

The server will run until the client disconnects or the process is terminated.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMCPServer(cmd)
		},
		Annotations: map[string]string{
			annotations.AnnotationExclude: "true",
		},
	}

	return cmd
}

// runMCPServer starts the MCP server.
func runMCPServer(cmd *cobra.Command) error {
	ctx := context.Background()

	// Get root command for tool generation
	rootCmd := cmd.Root()

	// Get version from root command annotation
	version := "dev"

	if rootCmd.Annotations != nil {
		if v, ok := rootCmd.Annotations["version"]; ok {
			version = v
		}
	}

	// Create server configuration
	cfg := mcpsvc.DefaultConfig(rootCmd, version)

	// Set working directory to current directory
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	cfg.WorkingDirectory = workDir

	// Run server - blocks until client disconnects
	err = mcpsvc.RunServer(ctx, cfg)
	if err != nil {
		return fmt.Errorf("running MCP server: %w", err)
	}

	return nil
}
