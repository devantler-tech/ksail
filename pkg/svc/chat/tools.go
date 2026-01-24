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

// File permission constants for secure file operations.
const (
	dirPermissions  = 0o750 // rwxr-x---
	filePermissions = 0o600 // rw-------
	maxFileSize     = 50000 // Maximum file size for read operations
)

// errPathAccessDenied is returned when a path is outside the working directory.
var errPathAccessDenied = fmt.Errorf("access denied: path must be within working directory")

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

// --- Tool Result Helpers ---

// failureResult creates a failure ToolResult with the given message.
func failureResult(message string) copilot.ToolResult {
	return copilot.ToolResult{
		TextResultForLLM: message,
		ResultType:       "failure",
	}
}

// successResult creates a success ToolResult with the given message.
func successResult(message string) copilot.ToolResult {
	return copilot.ToolResult{
		TextResultForLLM: message,
		ResultType:       "success",
	}
}

// --- Parameter Extraction Helpers ---

// extractParams extracts map parameters from tool invocation.
// Returns params map and ok flag.
func extractParams(invocation copilot.ToolInvocation) (map[string]any, bool) {
	params, ok := invocation.Arguments.(map[string]any)

	return params, ok
}

// extractStringParam extracts a string parameter from params map.
func extractStringParam(params map[string]any, key string) string {
	val, _ := params[key].(string)

	return val
}

// --- JSON Schema Helpers ---

// pathProperty returns the JSON schema for a file/directory path parameter.
func pathProperty() map[string]any {
	return map[string]any{
		"type":        "string",
		"description": "Path to file (relative or absolute, must be within working directory)",
	}
}

// extractAndValidatePath extracts path from params and validates it securely.
// Returns the secure path and error (as ToolResult if failed).
func extractAndValidatePath(params map[string]any, required bool) (string, *copilot.ToolResult) {
	path := extractStringParam(params, "path")

	if path == "" {
		if required {
			result := failureResult("Path is required")

			return "", &result
		}

		path = "."
	}

	safePath, err := securePath(path)
	if err != nil {
		result := failureResult(fmt.Sprintf("Path error: %v", err))

		return "", &result
	}

	return safePath, nil
}

// --- Handler Wrappers ---

// pathHandler is a tool handler that receives extracted params and validated path.
type pathHandler func(params map[string]any, safePath string) (copilot.ToolResult, error)

// withPathHandler wraps a handler with common param/path extraction boilerplate.
func withPathHandler(
	pathRequired bool,
	handler pathHandler,
) func(copilot.ToolInvocation) (copilot.ToolResult, error) {
	return func(invocation copilot.ToolInvocation) (copilot.ToolResult, error) {
		params, ok := extractParams(invocation)
		if !ok {
			return failureResult("Invalid parameters"), nil
		}

		safePath, errResult := extractAndValidatePath(params, pathRequired)
		if errResult != nil {
			return *errResult, nil
		}

		return handler(params, safePath)
	}
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
				"path": pathProperty(),
			},
			"required": []string{"path"},
		},
		Handler: withPathHandler(
			true,
			func(_ map[string]any, safePath string) (copilot.ToolResult, error) {
				content, err := os.ReadFile(safePath)
				if err != nil {
					return failureResult(fmt.Sprintf("Error reading file: %v", err)), nil
				}

				result := string(content)
				if len(result) > maxFileSize {
					result = result[:maxFileSize] + "\n... [truncated]"
				}

				return successResult(result), nil
			},
		),
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
				"path": pathProperty(),
			},
		},
		Handler: withPathHandler(
			false,
			func(_ map[string]any, safePath string) (copilot.ToolResult, error) {
				entries, err := os.ReadDir(safePath)
				if err != nil {
					return failureResult(fmt.Sprintf("Error listing directory: %v", err)), nil
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

				return successResult(result.String()), nil
			},
		),
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
				"path": pathProperty(),
				"content": map[string]any{
					"type":        "string",
					"description": "Content to write to the file",
				},
			},
			"required": []string{"path", "content"},
		},
		Handler: withPathHandler(
			true,
			func(params map[string]any, safePath string) (copilot.ToolResult, error) {
				content := extractStringParam(params, "content")

				// Create parent directories if needed
				dir := filepath.Dir(safePath)
				if err := os.MkdirAll(dir, dirPermissions); err != nil {
					return failureResult(fmt.Sprintf("Error creating directory: %v", err)), nil
				}

				// Write the file
				if err := os.WriteFile(safePath, []byte(content), filePermissions); err != nil {
					return failureResult(fmt.Sprintf("Error writing file: %v", err)), nil
				}

				msg := fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), safePath)

				return successResult(msg), nil
			},
		),
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
