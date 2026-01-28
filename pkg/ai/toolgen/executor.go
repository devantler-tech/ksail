package toolgen

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// ExecuteTool executes a tool definition with the given parameters.
// It sends output chunks via the opts.OutputChan if set.
// Returns the combined stdout/stderr output and any error.
func ExecuteTool(
	ctx context.Context,
	tool ToolDefinition,
	params map[string]any,
	opts ToolOptions,
) (string, error) {
	// Build command line arguments
	args, err := BuildCommandArgs(tool, params)
	if err != nil {
		return "", fmt.Errorf("building command args: %w", err)
	}

	// Execute command with tool name for output correlation
	return executeCommand(ctx, tool.CommandParts[0], args, tool.Name, opts)
}

// BuildCommandArgs constructs command-line arguments from parameters.
func BuildCommandArgs(tool ToolDefinition, params map[string]any) ([]string, error) {
	args := make([]string, 0)

	// Handle consolidated tools
	if tool.IsConsolidated {
		processedArgs, filteredParams, err := handleConsolidatedTool(tool, params)
		if err != nil {
			return nil, err
		}

		args = append(args, processedArgs...)
		params = filteredParams
	} else if len(tool.CommandParts) > 1 {
		// Add subcommands (skip the root command name)
		args = append(args, tool.CommandParts[1:]...)
	}

	// Process parameters
	for name, value := range params {
		// Handle positional args separately
		if name == "args" {
			positionalArgs, ok := value.([]any)
			if !ok {
				return nil, ErrArgsNotArray
			}

			for _, arg := range positionalArgs {
				args = append(args, fmt.Sprintf("%v", arg))
			}

			continue
		}

		// Format flag arguments
		flagArgs := formatFlagArg(name, value)
		args = append(args, flagArgs...)
	}

	return args, nil
}

// handleConsolidatedTool extracts subcommand info and returns processed args and filtered params.
func handleConsolidatedTool(
	tool ToolDefinition,
	params map[string]any,
) ([]string, map[string]any, error) {
	// Extract subcommand parameter
	subcommandName, ok := params[tool.SubcommandParam].(string)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %s", ErrMissingSubcommandParam, tool.SubcommandParam)
	}

	// Look up subcommand definition
	subcommandDef, exists := tool.Subcommands[subcommandName]
	if !exists {
		validSubcommands := make([]string, 0, len(tool.Subcommands))
		for name := range tool.Subcommands {
			validSubcommands = append(validSubcommands, name)
		}

		return nil, nil, fmt.Errorf(
			"%w: %s=%s (valid options: %s)",
			ErrInvalidSubcommand,
			tool.SubcommandParam,
			subcommandName,
			strings.Join(validSubcommands, ", "),
		)
	}

	// Build args from subcommand's CommandParts (skip the root command name)
	var args []string
	if len(subcommandDef.CommandParts) > 1 {
		args = subcommandDef.CommandParts[1:]
	}

	// Remove subcommand parameter from params before processing flags
	filteredParams := make(map[string]any)

	for key, val := range params {
		if key != tool.SubcommandParam {
			filteredParams[key] = val
		}
	}

	return args, filteredParams, nil
}

// formatFlagArg formats a single flag into command-line arguments.
func formatFlagArg(name string, value any) []string {
	switch typedValue := value.(type) {
	case bool:
		if typedValue {
			return []string{"--" + name}
		}

		return nil // Don't include false boolean flags
	case []any:
		// Array values: --flag=value1 --flag=value2
		args := make([]string, 0, len(typedValue))

		for _, item := range typedValue {
			args = append(args, fmt.Sprintf("--%s=%v", name, item))
		}

		return args
	case nil:
		return nil // Skip nil values
	default:
		// Single value: --flag=value
		return []string{fmt.Sprintf("--%s=%v", name, value)}
	}
}

// executeCommand runs the command and streams output.
// Returns the combined stdout/stderr output and any error.
func executeCommand(
	ctx context.Context,
	command string,
	args []string,
	toolName string,
	opts ToolOptions,
) (string, error) {
	// Create context with timeout.
	// If opts.CommandTimeout is 0 or negative, no timeout is applied.
	execCtx := ctx

	if opts.CommandTimeout > 0 {
		var cancel context.CancelFunc

		execCtx, cancel = context.WithTimeout(ctx, opts.CommandTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(execCtx, command, args...)
	cmd.Dir = opts.WorkingDirectory

	// If no output channel, just run and return
	if opts.OutputChan == nil {
		output, err := cmd.CombinedOutput()
		if err != nil {
			return string(output), fmt.Errorf("command failed: %w", err)
		}

		return string(output), nil
	}

	// Run with streaming output
	return executeWithStreaming(cmd, toolName, opts.OutputChan)
}

// executeWithStreaming sets up pipes and streams command output.
func executeWithStreaming(
	cmd *exec.Cmd,
	toolName string,
	outputChan chan<- OutputChunk,
) (string, error) {
	// Set up streaming output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("creating stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("creating stderr pipe: %w", err)
	}

	// Start command
	err = cmd.Start()
	if err != nil {
		return "", fmt.Errorf("starting command: %w", err)
	}

	// Stream output with synchronization and accumulate for return
	var (
		outputBuffer strings.Builder
		bufferMutex  sync.Mutex
		waitGroup    sync.WaitGroup
	)

	waitGroup.Go(func() {
		streamOutput(streamConfig{
			pipeReader: stdout,
			source:     "stdout",
			toolName:   toolName,
			outputChan: outputChan,
			buffer:     &outputBuffer,
			mutex:      &bufferMutex,
		})
	})

	waitGroup.Go(func() {
		streamOutput(streamConfig{
			pipeReader: stderr,
			source:     "stderr",
			toolName:   toolName,
			outputChan: outputChan,
			buffer:     &outputBuffer,
			mutex:      &bufferMutex,
		})
	})

	// Wait for all output to be read before waiting for command
	waitGroup.Wait()

	// Wait for completion
	err = cmd.Wait()
	if err != nil {
		return outputBuffer.String(), fmt.Errorf("command failed: %w", err)
	}

	return outputBuffer.String(), nil
}

// streamConfig holds configuration for streaming output.
type streamConfig struct {
	pipeReader io.Reader
	source     string
	toolName   string
	outputChan chan<- OutputChunk
	buffer     *strings.Builder
	mutex      *sync.Mutex
}

// streamOutput reads from a reader and sends chunks to the output channel.
// Also accumulates output in the provided buffer for returning to the LLM.
//
// Note: Uses bufio.Scanner which has a default max token size of 64KB.
// For KSail use cases (command output), this is acceptable. If extremely long lines
// are encountered, Scanner.Scan() will fail and stop reading. The command will still
// complete, but output may be truncated. If this becomes an issue, consider using
// bufio.Reader.ReadString('\n') or scanner.Buffer() to increase the limit.
func streamOutput(cfg streamConfig) {
	scanner := bufio.NewScanner(cfg.pipeReader)

	for scanner.Scan() {
		line := scanner.Text() + "\n"

		// Send to channel for UI display
		cfg.outputChan <- OutputChunk{
			ToolID: cfg.toolName,
			Source: cfg.source,
			Chunk:  line,
		}

		// Accumulate for LLM (thread-safe)
		cfg.mutex.Lock()
		cfg.buffer.WriteString(line)
		cfg.mutex.Unlock()
	}
}

// ToolParametersFromJSON unmarshals JSON parameters into a map.
func ToolParametersFromJSON(jsonParams string) (map[string]any, error) {
	var params map[string]any

	err := json.Unmarshal([]byte(jsonParams), &params)
	if err != nil {
		return nil, fmt.Errorf("unmarshaling parameters: %w", err)
	}

	return params, nil
}

// FormatToolName formats a tool name from a command path.
// Example: "ksail cluster create" -> "ksail_cluster_create".
func FormatToolName(commandPath string) string {
	return strings.ReplaceAll(commandPath, " ", "_")
}
