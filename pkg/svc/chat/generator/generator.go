// Package generator provides automatic tool generation from Cobra commands.
// It traverses the command tree and creates Copilot SDK tools with JSON schema parameters.
package generator

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// enumValuer is implemented by flag Value types that provide valid enum values.
// This matches the EnumValuer interface in pkg/apis/cluster/v1alpha1/enums.go
type enumValuer interface {
	ValidValues() []string
}

// defaulter is implemented by flag Value types that provide a default value.
type defaulter interface {
	Default() any
}

const (
	// AnnotationExclude marks a command to be excluded from tool generation.
	AnnotationExclude = "chat.exclude"
	// AnnotationPermission marks a command as requiring permission approval.
	// Value should be "destructive" for dangerous operations.
	AnnotationPermission = "chat.permission"
	// AnnotationDescription provides a custom description for the tool.
	// If not set, cmd.Short is used.
	AnnotationDescription = "chat.description"
)

// ToolOptions configures tool generation behavior.
type ToolOptions struct {
	// ExcludeCommands is a list of command paths to exclude (e.g., "ksail chat").
	ExcludeCommands []string
	// IncludeHidden includes hidden commands in tool generation.
	IncludeHidden bool
	// CommandTimeout is the timeout for command execution.
	CommandTimeout time.Duration
	// OutputChan receives real-time output chunks from running commands.
	// If nil, output is only available after command completion.
	OutputChan chan<- OutputChunk
}

// OutputChunk represents a chunk of output from a running command.
type OutputChunk struct {
	ToolID string
	Chunk  string
}

// DefaultOptions returns sensible default options for tool generation.
func DefaultOptions() ToolOptions {
	return ToolOptions{
		ExcludeCommands: []string{
			"ksail chat",       // Don't expose chat command as a tool
			"ksail completion", // Shell completion not useful as tool
		},
		IncludeHidden:  false,
		CommandTimeout: 60 * time.Second,
	}
}

// GenerateToolsFromCommand traverses a Cobra command tree and generates tools.
// It returns a slice of Copilot SDK tools for all runnable leaf commands.
func GenerateToolsFromCommand(root *cobra.Command, opts ToolOptions) []copilot.Tool {
	var tools []copilot.Tool
	generateToolsRecursive(root, &tools, opts)

	return tools
}

// generateToolsRecursive traverses the command tree depth-first.
func generateToolsRecursive(cmd *cobra.Command, tools *[]copilot.Tool, opts ToolOptions) {
	// Skip excluded commands
	if shouldExclude(cmd, opts) {
		return
	}

	// Check for explicit exclusion annotation
	if cmd.Annotations != nil && cmd.Annotations[AnnotationExclude] == "true" {
		return
	}

	// If command has subcommands, traverse them
	if len(cmd.Commands()) > 0 {
		for _, subCmd := range cmd.Commands() {
			generateToolsRecursive(subCmd, tools, opts)
		}
		// Also check if this command is runnable itself (has RunE and isn't just a group)
		if !isRunnableCommand(cmd) {
			return
		}
	}

	// Generate tool for runnable commands
	if isRunnableCommand(cmd) {
		tool := commandToTool(cmd, opts)
		*tools = append(*tools, tool)
	}
}

// shouldExclude checks if a command should be excluded from tool generation.
func shouldExclude(cmd *cobra.Command, opts ToolOptions) bool {
	// Check hidden commands
	if cmd.Hidden && !opts.IncludeHidden {
		return true
	}

	// Check exclusion list
	cmdPath := cmd.CommandPath()
	return slices.Contains(opts.ExcludeCommands, cmdPath)
}

// isRunnableCommand checks if a command can actually be executed.
// Commands that only display help are not considered runnable.
func isRunnableCommand(cmd *cobra.Command) bool {
	// Must have either Run or RunE
	if cmd.Run == nil && cmd.RunE == nil {
		return false
	}

	// Skip commands that just show help (common pattern for group commands)
	// We detect this by checking if the command has subcommands and its RunE
	// just calls Help()
	if len(cmd.Commands()) > 0 && cmd.RunE != nil {
		// This is a heuristic - group commands typically only call Help()
		// We'll include it if it has flags beyond the standard help flag
		hasNonHelpFlags := false

		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Name != "help" {
				hasNonHelpFlags = true
			}
		})

		if !hasNonHelpFlags {
			return false
		}
	}

	return true
}

// commandToTool converts a Cobra command to a Copilot SDK tool.
func commandToTool(cmd *cobra.Command, opts ToolOptions) copilot.Tool {
	// Build tool name: "ksail cluster create" -> "ksail_cluster_create"
	toolName := strings.ReplaceAll(cmd.CommandPath(), " ", "_")

	// Get description from annotation or Short
	description := cmd.Short
	if cmd.Annotations != nil && cmd.Annotations[AnnotationDescription] != "" {
		description = cmd.Annotations[AnnotationDescription]
	}
	// Append Long description if available and different
	if cmd.Long != "" && cmd.Long != cmd.Short {
		description = description + "\n\n" + cmd.Long
	}

	// Build JSON schema from flags
	parameters := buildParameterSchema(cmd)

	// Build the handler
	handler := buildHandler(cmd, opts)

	return copilot.Tool{
		Name:        toolName,
		Description: description,
		Parameters:  parameters,
		Handler:     handler,
	}
}

// buildParameterSchema creates a JSON schema from Cobra command flags.
func buildParameterSchema(cmd *cobra.Command) map[string]any {
	properties := make(map[string]any)
	required := []string{}

	// Visit all flags (local and persistent)
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		// Skip help flag
		if f.Name == "help" {
			return
		}

		prop := flagToSchemaProperty(f)
		properties[f.Name] = prop

		// Mark as required if no default value and not a bool
		// Bools default to false so they're never truly "required"
		if f.DefValue == "" && f.Value.Type() != "bool" {
			required = append(required, f.Name)
		}
	})

	// Check for positional arguments
	if cmd.Args != nil {
		// Add positional args parameter for commands that expect them
		// We'll use a generic "args" parameter
		properties["args"] = map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Positional arguments for the command",
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

// flagToSchemaProperty converts a pflag to a JSON schema property.
func flagToSchemaProperty(f *pflag.Flag) map[string]any {
	prop := map[string]any{}

	// Check if the flag value implements enumValuer interface
	if ev, ok := f.Value.(enumValuer); ok {
		validValues := ev.ValidValues()
		if len(validValues) > 0 {
			// Add enum constraint for LLM to know valid options
			prop["type"] = "string"
			prop["enum"] = validValues
			prop["description"] = fmt.Sprintf("%s (valid options: %s)", f.Usage, strings.Join(validValues, ", "))

			// Check if it also implements defaulter
			if d, ok := f.Value.(defaulter); ok {
				if def := d.Default(); def != nil {
					prop["default"] = fmt.Sprintf("%v", def)
				}
			} else if f.DefValue != "" {
				prop["default"] = f.DefValue
			}

			return prop
		}
	}

	// Standard description
	prop["description"] = f.Usage

	// Map pflag types to JSON schema types
	switch f.Value.Type() {
	case "bool":
		prop["type"] = "boolean"
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		prop["type"] = "integer"
	case "float32", "float64":
		prop["type"] = "number"
	case "stringSlice", "stringArray":
		prop["type"] = "array"
		prop["items"] = map[string]any{"type": "string"}
	case "intSlice":
		prop["type"] = "array"
		prop["items"] = map[string]any{"type": "integer"}
	case "duration":
		prop["type"] = "string"
		prop["description"] = f.Usage + " (format: 1h30m, 5m, 30s)"
	default:
		// Default to string for unknown types
		prop["type"] = "string"
	}

	// Add default value if present
	if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "[]" {
		prop["default"] = f.DefValue
	}

	return prop
}

// buildHandler creates a tool handler that executes the Cobra command.
func buildHandler(cmd *cobra.Command, opts ToolOptions) copilot.ToolHandler {
	// Capture the command path for execution
	cmdPath := cmd.CommandPath()
	// Split into parts: "ksail cluster create" -> ["ksail", "cluster", "create"]
	cmdParts := strings.Fields(cmdPath)
	// Tool name for correlation: "ksail cluster create" -> "ksail_cluster_create"
	toolName := strings.ReplaceAll(cmdPath, " ", "_")

	return func(invocation copilot.ToolInvocation) (copilot.ToolResult, error) {
		// Build command arguments from invocation
		args := buildCommandArgs(cmd, invocation, cmdParts)

		// Build the full command string for reporting
		fullCmd := "ksail"
		if len(args) > 0 {
			fullCmd += " " + strings.Join(args, " ")
		}

		// Execute the command with streaming output
		output, err := runKSailCommand(args, toolName, opts)
		if err != nil {
			return copilot.ToolResult{
				TextResultForLLM: fmt.Sprintf(
					"Command: %s\nStatus: FAILED\nError: %v\nOutput:\n%s",
					fullCmd,
					err,
					output,
				),
				ResultType: "failure",
				SessionLog: fmt.Sprintf("[FAILED] %s: %v", fullCmd, err),
			}, nil
		}

		// Format output with clear structure for the LLM
		resultText := output
		if resultText == "" {
			resultText = "(no output)"
		}

		return copilot.ToolResult{
			TextResultForLLM: fmt.Sprintf(
				"Command: %s\nStatus: SUCCESS\nOutput:\n%s",
				fullCmd,
				resultText,
			),
			ResultType: "success",
			SessionLog: fmt.Sprintf("[SUCCESS] %s", fullCmd),
		}, nil
	}
}

// buildCommandArgs converts tool invocation arguments to CLI arguments.
func buildCommandArgs(
	cmd *cobra.Command,
	invocation copilot.ToolInvocation,
	cmdParts []string,
) []string {
	// Start with subcommand parts (excluding "ksail" as it's the binary name)
	args := make([]string, 0, len(cmdParts)-1)
	if len(cmdParts) > 1 {
		args = append(args, cmdParts[1:]...)
	}

	params, ok := invocation.Arguments.(map[string]any)
	if !ok {
		return args
	}

	// Convert each parameter to CLI flag format
	for name, value := range params {
		// Handle positional args specially
		if name == "args" {
			if arr, ok := value.([]any); ok {
				for _, v := range arr {
					if s, ok := v.(string); ok {
						args = append(args, s)
					}
				}
			}

			continue
		}

		// Look up the flag to get its type
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			continue
		}

		// Format flag based on type
		switch v := value.(type) {
		case bool:
			if v {
				args = append(args, "--"+name)
			}
		case string:
			// Skip empty strings - they're invalid for most flags
			// especially enum-type flags that require valid values
			if v != "" {
				args = append(args, fmt.Sprintf("--%s=%v", name, v))
			}
		case []any:
			// Handle slice flags
			for _, item := range v {
				args = append(args, fmt.Sprintf("--%s=%v", name, item))
			}
		default:
			args = append(args, fmt.Sprintf("--%s=%v", name, v))
		}
	}

	return args
}

// runKSailCommand executes a KSail command with the given arguments.
// It streams output in real-time through the OutputChan if configured.
func runKSailCommand(args []string, toolName string, opts ToolOptions) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), opts.CommandTimeout)
	defer cancel()

	// Find the ksail binary
	ksailPath, err := findKSailBinary()
	if err != nil {
		return "", fmt.Errorf("failed to find ksail binary: %w", err)
	}

	cmd := exec.CommandContext(ctx, ksailPath, args...)

	// If no output channel, use simple CombinedOutput
	if opts.OutputChan == nil {
		output, execErr := cmd.CombinedOutput()
		if ctx.Err() == context.DeadlineExceeded {
			return string(output), fmt.Errorf("command timed out after %v", opts.CommandTimeout)
		}

		return string(output), execErr
	}

	// Set up pipes for streaming output
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start command: %w", err)
	}

	// Collect all output while streaming
	var (
		outputBuilder strings.Builder
		mutex         sync.Mutex
		wg            sync.WaitGroup
	)

	// Stream stdout
	wg.Add(1)

	go func() {
		defer wg.Done()

		reader := bufio.NewReader(stdoutPipe)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				mutex.Lock()
				outputBuilder.WriteString(line)
				mutex.Unlock()
				// Send chunk to channel (non-blocking to avoid deadlock)
				select {
				case opts.OutputChan <- OutputChunk{ToolID: toolName, Chunk: line}:
				default:
					// Channel full, skip this chunk
				}
			}

			if err != nil {
				break
			}
		}
	}()

	// Stream stderr
	wg.Add(1)

	go func() {
		defer wg.Done()

		reader := bufio.NewReader(stderrPipe)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				mutex.Lock()
				outputBuilder.WriteString(line)
				mutex.Unlock()
				// Send chunk to channel (non-blocking to avoid deadlock)
				select {
				case opts.OutputChan <- OutputChunk{ToolID: toolName, Chunk: line}:
				default:
					// Channel full, skip this chunk
				}
			}

			if err != nil {
				break
			}
		}
	}()

	// Wait for pipes to be fully read before Wait() closes them
	wg.Wait()

	// Wait for command to complete
	waitErr := cmd.Wait()

	if ctx.Err() == context.DeadlineExceeded {
		return outputBuilder.String(), fmt.Errorf("command timed out after %v", opts.CommandTimeout)
	}

	return outputBuilder.String(), waitErr
}

// findKSailBinary locates the ksail binary.
func findKSailBinary() (string, error) {
	// First, check if we're running as the ksail binary itself
	executable, err := os.Executable()
	if err == nil {
		// If the executable is named "ksail", use it
		if strings.HasSuffix(executable, "ksail") || strings.Contains(executable, "ksail") {
			return executable, nil
		}
	}

	// Try to find ksail in PATH
	path, err := exec.LookPath("ksail")
	if err == nil {
		return path, nil
	}

	// Try common locations
	commonPaths := []string{
		"./ksail",
		"/usr/local/bin/ksail",
		"/opt/homebrew/bin/ksail",
	}

	for _, p := range commonPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("ksail binary not found in PATH or common locations")
}
