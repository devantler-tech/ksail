package chat

import (
	"strings"
	"time"
)

// toolStatus represents the current state of a tool execution.
type toolStatus int

const (
	toolRunning toolStatus = iota
	toolSuccess
	toolFailed
)

// toolExecution tracks a single tool invocation.
type toolExecution struct {
	id           string
	name         string
	command      string // The actual command being executed (e.g., "ksail cluster list --all")
	status       toolStatus
	output       string
	expanded     bool // whether output is expanded in the view
	startTime    time.Time
	textPosition int // position in assistant response when tool was called
}

// humanizeToolName converts snake_case tool names to readable format using the given mappings.
// The generic fallback (snake_case → Title Case) is used for unmapped names.
func humanizeToolName(name string, mappings map[string]string) string {
	if mapped, ok := mappings[name]; ok {
		return mapped
	}

	// Fallback: convert snake_case to Title Case
	words := strings.Split(name, "_")
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}

	return strings.Join(words, " ")
}

// extractCommandFromArgs extracts a command string from tool arguments for display.
// It uses the provided command builders to construct a human-readable command string.
func extractCommandFromArgs(toolName string, args any, builders map[string]CommandBuilder) string {
	argsMap, ok := args.(map[string]any)
	if !ok {
		return ""
	}

	if builder, exists := builders[toolName]; exists {
		return builder(argsMap)
	}

	return ""
}
