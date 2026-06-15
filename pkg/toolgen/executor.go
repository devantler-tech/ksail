package toolgen

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"sync"
)

// executeTool executes a tool definition with the given parameters.
// It sends output chunks via the opts.OutputChan if set.
// Returns the combined stdout/stderr output (with warnings about ignored
// parameters appended) and any error.
func executeTool(
	ctx context.Context,
	tool ToolDefinition,
	params map[string]any,
	opts ToolOptions,
) (string, error) {
	// Build command line arguments
	args, warnings, err := buildCommandArgsAndWarnings(tool, params)
	if err != nil {
		return "", fmt.Errorf("building command args: %w", err)
	}

	// Execute command with tool name for output correlation
	output, err := executeCommand(ctx, resolveCommand(opts, tool), args, tool.Name, opts)

	return appendWarnings(output, warnings), err
}

// resolveCommand returns the executable to run for a tool invocation:
// opts.ExecutablePath when set (the MCP server sets it to the running binary
// via DefaultExecutablePath/os.Executable, removing any PATH dependence),
// otherwise the tool's recorded root command name as a PATH-lookup fallback.
func resolveCommand(opts ToolOptions, tool ToolDefinition) string {
	if opts.ExecutablePath != "" {
		return opts.ExecutablePath
	}

	return tool.CommandParts[0]
}

// appendWarnings appends warning lines to command output so MCP/chat clients
// see them alongside the command result.
func appendWarnings(output string, warnings []string) string {
	if len(warnings) == 0 {
		return output
	}

	var builder strings.Builder

	builder.WriteString(output)

	if output != "" && !strings.HasSuffix(output, "\n") {
		builder.WriteString("\n")
	}

	for _, warning := range warnings {
		builder.WriteString("Warning: ")
		builder.WriteString(warning)
		builder.WriteString("\n")
	}

	return builder.String()
}

// BuildCommandArgs constructs command-line arguments from parameters.
func BuildCommandArgs(tool ToolDefinition, params map[string]any) ([]string, error) {
	args, _, err := buildCommandArgsAndWarnings(tool, params)

	return args, err
}

// buildCommandArgsAndWarnings constructs command-line arguments from parameters.
// The returned warnings describe parameters that were ignored (e.g. flags not
// applicable to the selected subcommand of a consolidated tool).
func buildCommandArgsAndWarnings(
	tool ToolDefinition,
	params map[string]any,
) ([]string, []string, error) {
	args := make([]string, 0)

	var warnings []string

	// Handle consolidated tools
	if tool.IsConsolidated {
		processedArgs, filteredParams, consolidatedWarnings, err := handleConsolidatedTool(
			tool, params,
		)
		if err != nil {
			return nil, nil, err
		}

		args = append(args, processedArgs...)
		params = filteredParams
		warnings = consolidatedWarnings
	} else if len(tool.CommandParts) > 1 {
		// Add subcommands (skip the root command name)
		args = append(args, tool.CommandParts[1:]...)
	}

	// Process parameters
	for name, value := range params {
		// Handle positional args separately
		if name == argsKey {
			positionalArgs, ok := value.([]any)
			if !ok {
				return nil, nil, ErrArgsNotArray
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

	return args, warnings, nil
}

// handleConsolidatedTool extracts subcommand info and returns processed args,
// filtered params, and warnings describing ignored parameters.
func handleConsolidatedTool(
	tool ToolDefinition,
	params map[string]any,
) ([]string, map[string]any, []string, error) {
	// Extract subcommand parameter
	subcommandName, ok := params[tool.SubcommandParam].(string)
	if !ok {
		return nil, nil, nil, fmt.Errorf("%w: %s", ErrMissingSubcommandParam, tool.SubcommandParam)
	}

	// Look up subcommand definition
	subcommandDef, exists := tool.Subcommands[subcommandName]
	if !exists {
		validSubcommands := make([]string, 0, len(tool.Subcommands))
		for name := range tool.Subcommands {
			validSubcommands = append(validSubcommands, name)
		}

		return nil, nil, nil, fmt.Errorf(
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

	filteredParams, warnings, err := filterConsolidatedParams(
		tool, subcommandDef, subcommandName, params,
	)
	if err != nil {
		return nil, nil, nil, err
	}

	return args, filteredParams, warnings, nil
}

// filterConsolidatedParams filters params to only the flags that apply to the
// selected subcommand. This prevents "unknown flag" errors when MCP clients
// pass default values for flags that belong to other subcommands in the
// consolidated tool's union schema. Ignored flags are reported as a warning
// (appended to the tool output) instead of being dropped silently, so clients
// don't believe an inapplicable parameter took effect.
func filterConsolidatedParams(
	tool ToolDefinition,
	subcommandDef *SubcommandDef,
	subcommandName string,
	params map[string]any,
) (map[string]any, []string, error) {
	filteredParams := make(map[string]any)

	var ignored []string

	for key, val := range params {
		// Skip the subcommand selector parameter
		if key == tool.SubcommandParam {
			continue
		}

		// Only forward positional args if the subcommand accepts them
		if key == argsKey {
			if !subcommandDef.AcceptsArgs {
				return nil, nil, fmt.Errorf(
					"%w: %s does not accept positional arguments",
					ErrArgsNotAccepted,
					subcommandName,
				)
			}

			filteredParams[key] = val

			continue
		}

		// Only include flags that exist in the selected subcommand's flag definitions
		if _, appliesToSubcommand := subcommandDef.Flags[key]; appliesToSubcommand {
			filteredParams[key] = val
		} else {
			ignored = append(ignored, key)
		}
	}

	var warnings []string

	if len(ignored) > 0 {
		slices.Sort(ignored)
		warnings = append(warnings, fmt.Sprintf(
			"ignored parameters not applicable to subcommand %q: %s",
			subcommandName,
			strings.Join(ignored, ", "),
		))
	}

	return filteredParams, warnings, nil
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

	cmd := exec.CommandContext( //nolint:gosec // G204: command from trusted config
		execCtx,
		command,
		args...)
	cmd.Dir = opts.WorkingDirectory

	// Log command execution for debugging
	if opts.Logger != nil {
		opts.Logger.Debug("executing command",
			"command", command,
			"args", args,
			"workdir", opts.WorkingDirectory,
			"tool", toolName)
	}

	// If no output channel, just run and return
	if opts.OutputChan == nil {
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Log failure with captured output
			if opts.Logger != nil {
				opts.Logger.Error("command failed",
					"command", command,
					"args", args,
					"output", string(output),
					"error", err)
			}

			return string(output), fmt.Errorf("command failed: %w", err)
		}

		if opts.Logger != nil {
			opts.Logger.Debug("command completed successfully",
				"command", command,
				"args", args)
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
// Scanner Limit Behavior:
// Uses bufio.Scanner with a default max token (line) size of 64KB. When a line exceeds
// this limit, Scanner.Scan() returns false and scanning stops silently. This results in:
//   - Partial output being sent to the UI and LLM (all lines before the too-long line)
//   - The command continues executing but remaining output is not captured
//   - No error is returned to the caller (output truncation is silent)
//
// For typical KSail command output, 64KB per line is sufficient. If truncation occurs,
// increase the buffer with scanner.Buffer() or switch to bufio.Reader.ReadString('\n').
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
// Example: "ksail cluster create" -> "cluster_create".
func FormatToolName(commandPath string) string {
	strippedPath := stripRootCommand(commandPath)

	return strings.ReplaceAll(strippedPath, " ", "_")
}
