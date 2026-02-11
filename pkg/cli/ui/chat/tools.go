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

// humanizeToolName converts snake_case tool names to readable format.
func humanizeToolName(name string) string {
	// Common tool name mappings for better readability
	mappings := map[string]string{
		"report_intent":        "Analyzing request",
		"ksail_cluster_list":   "Listing clusters",
		"ksail_cluster_info":   "Getting cluster info",
		"ksail_cluster_create": "Creating cluster",
		"ksail_cluster_delete": "Deleting cluster",
		"bash":                 "Running command",
		"read_file":            "Reading file",
		"write_file":           "Writing file",
		"list_dir":             "Listing directory",
	}
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

// commandBuilder extracts a display command from tool arguments.
type commandBuilder func(args map[string]any) string

// commandBuilders maps tool names to their command extraction logic.
var commandBuilders = map[string]commandBuilder{
	"ksail_cluster_list": buildClusterListCommand,
	"ksail_cluster_info": buildClusterInfoCommand,
	"ksail_workload_get": buildWorkloadGetCommand,
	"read_file":          buildReadFileCommand,
	"list_directory":     buildListDirectoryCommand,
}

// extractCommandFromArgs extracts a command string from tool arguments for display.
// This helps users understand exactly what command is being executed.
func extractCommandFromArgs(toolName string, args any) string {
	argsMap, ok := args.(map[string]any)
	if !ok {
		return ""
	}

	if builder, exists := commandBuilders[toolName]; exists {
		return builder(argsMap)
	}

	return ""
}

func buildClusterListCommand(args map[string]any) string {
	cmd := "ksail cluster list"
	if all, ok := args["all"].(bool); ok && all {
		cmd += " --all"
	}

	return cmd
}

func buildClusterInfoCommand(args map[string]any) string {
	cmd := "ksail cluster info"
	if name, ok := args["name"].(string); ok && name != "" {
		cmd += " --name " + name
	}

	return cmd
}

func buildWorkloadGetCommand(args map[string]any) string {
	resource, _ := args["resource"].(string)
	if resource == "" {
		return ""
	}

	cmd := "ksail workload get " + resource

	if name, ok := args["name"].(string); ok && name != "" {
		cmd += " " + name
	}

	if ns, ok := args["namespace"].(string); ok && ns != "" {
		cmd += " -n " + ns
	}

	if allNs, ok := args["all_namespaces"].(bool); ok && allNs {
		cmd += " -A"
	}

	if output, ok := args["output"].(string); ok && output != "" {
		cmd += " -o " + output
	}

	return cmd
}

func buildReadFileCommand(args map[string]any) string {
	if path, ok := args["path"].(string); ok && path != "" {
		return "cat " + path
	}

	return ""
}

func buildListDirectoryCommand(args map[string]any) string {
	path := "."
	if p, ok := args["path"].(string); ok && p != "" {
		path = p
	}

	return "ls " + path
}
