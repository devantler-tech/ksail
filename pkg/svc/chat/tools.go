package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/chat/generator"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/spf13/cobra"
)

// GetKSailTools returns the tools available to the chat assistant.
// It combines auto-generated tools from Cobra commands with manual file system tools.
// The rootCmd parameter should be the root Cobra command for the CLI.
// The outputChan parameter enables real-time output streaming (can be nil).
func GetKSailTools(rootCmd *cobra.Command, outputChan chan<- generator.OutputChunk) []copilot.Tool {
	// Generate tools from the Cobra command tree
	opts := generator.DefaultOptions()
	opts.OutputChan = outputChan
	generatedTools := generator.GenerateToolsFromCommand(rootCmd, opts)

	// Add manual file system tools that aren't part of the CLI
	fileSystemTools := []copilot.Tool{
		readFileTool(),
		listDirectoryTool(),
		writeFileTool(),
	}

	return append(generatedTools, fileSystemTools...)
}

// readFileTool reads the contents of a file.
func readFileTool() copilot.Tool {
	return copilot.Tool{
		Name: "read_file",
		Description: "Read file contents. Useful for examining ksail.yaml, " +
			"Kubernetes manifests, or config files. Only reads files within the working directory.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to file (relative or absolute, must be within working directory)",
				},
			},
			"required": []string{"path"},
		},
		Handler: func(invocation copilot.ToolInvocation) (copilot.ToolResult, error) {
			params, ok := invocation.Arguments.(map[string]any)
			if !ok {
				return copilot.ToolResult{
					TextResultForLLM: "Invalid parameters",
					ResultType:       "failure",
				}, nil
			}

			path, _ := params["path"].(string)
			if path == "" {
				return copilot.ToolResult{
					TextResultForLLM: "Path is required",
					ResultType:       "failure",
				}, nil
			}

			// Resolve and validate path security
			safePath, err := securePath(path)
			if err != nil {
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf("Path error: %v", err),
					ResultType:       "failure",
				}, nil
			}

			content, err := os.ReadFile(safePath)
			if err != nil {
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf("Error reading file: %v", err),
					ResultType:       "failure",
				}, nil
			}

			// Truncate if too large
			const maxSize = 50000

			result := string(content)
			if len(result) > maxSize {
				result = result[:maxSize] + "\n... [truncated]"
			}

			return copilot.ToolResult{
				TextResultForLLM: result,
				ResultType:       "success",
			}, nil
		},
	}
}

// listDirectoryTool lists the contents of a directory.
func listDirectoryTool() copilot.Tool {
	return copilot.Tool{
		Name: "list_directory",
		Description: "List directory contents. Useful for exploring project structure " +
			"and finding Kubernetes manifests. Only lists directories within the working directory.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to directory (relative or absolute, must be within working directory)",
				},
			},
		},
		Handler: func(invocation copilot.ToolInvocation) (copilot.ToolResult, error) {
			params, ok := invocation.Arguments.(map[string]any)
			if !ok {
				return copilot.ToolResult{
					TextResultForLLM: "Invalid parameters",
					ResultType:       "failure",
				}, nil
			}

			path, _ := params["path"].(string)
			if path == "" {
				path = "."
			}

			// Resolve and validate path security
			safePath, err := securePath(path)
			if err != nil {
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf("Path error: %v", err),
					ResultType:       "failure",
				}, nil
			}

			entries, err := os.ReadDir(safePath)
			if err != nil {
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf("Error listing directory: %v", err),
					ResultType:       "failure",
				}, nil
			}

			var result strings.Builder

			result.WriteString(fmt.Sprintf("Contents of %s:\n", safePath))

			for _, entry := range entries {
				indicator := ""
				if entry.IsDir() {
					indicator = "/"
				}

				result.WriteString(fmt.Sprintf("  %s%s\n", entry.Name(), indicator))
			}

			return copilot.ToolResult{
				TextResultForLLM: result.String(),
				ResultType:       "success",
			}, nil
		},
	}
}

// writeFileTool writes content to a file.
func writeFileTool() copilot.Tool {
	return copilot.Tool{
		Name: "write_file",
		Description: "Write content to a file. Useful for creating or modifying Kubernetes " +
			"manifests, configuration files, etc. Only writes files within the working directory.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to file (relative or absolute, must be within working directory)",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Content to write to the file",
				},
			},
			"required": []string{"path", "content"},
		},
		Handler: func(invocation copilot.ToolInvocation) (copilot.ToolResult, error) {
			params, ok := invocation.Arguments.(map[string]any)
			if !ok {
				return copilot.ToolResult{
					TextResultForLLM: "Invalid parameters",
					ResultType:       "failure",
				}, nil
			}

			path, _ := params["path"].(string)
			content, _ := params["content"].(string)

			if path == "" {
				return copilot.ToolResult{
					TextResultForLLM: "Path is required",
					ResultType:       "failure",
				}, nil
			}

			// Resolve and validate path security
			safePath, err := securePath(path)
			if err != nil {
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf("Path error: %v", err),
					ResultType:       "failure",
				}, nil
			}

			// Create parent directories if needed
			dir := filepath.Dir(safePath)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf("Error creating directory: %v", err),
					ResultType:       "failure",
				}, nil
			}

			// Write the file
			if err := os.WriteFile(safePath, []byte(content), 0o644); err != nil {
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf("Error writing file: %v", err),
					ResultType:       "failure",
				}, nil
			}

			return copilot.ToolResult{
				TextResultForLLM: fmt.Sprintf(
					"Successfully wrote %d bytes to %s",
					len(content),
					safePath,
				),
				ResultType: "success",
			}, nil
		},
	}
}

// securePath validates and resolves a path, ensuring it stays within the working directory.
// This prevents directory traversal attacks (e.g., ../../../etc/passwd).
func securePath(path string) (string, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("could not get working directory: %w", err)
	}

	// Resolve to absolute path
	var absPath string
	if filepath.IsAbs(path) {
		absPath = filepath.Clean(path)
	} else {
		absPath = filepath.Clean(filepath.Join(workDir, path))
	}

	// Ensure the resolved path is within or equal to the working directory
	if !strings.HasPrefix(absPath, workDir) {
		return "", errPathAccessDenied
	}

	return absPath, nil
}

// errPathAccessDenied is returned when a path is outside the working directory.
var errPathAccessDenied = fmt.Errorf("access denied: path must be within working directory")
