package chat

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	copilot "github.com/github/copilot-sdk/go"
)

// GetKSailTools returns the custom tools available to the chat assistant.
// These tools wrap KSail commands and provide structured access to cluster operations.
func GetKSailTools() []copilot.Tool {
	return []copilot.Tool{
		clusterListTool(),
		clusterInfoTool(),
		workloadGetTool(),
		readFileTool(),
		listDirectoryTool(),
	}
}

// clusterListTool lists all KSail clusters.
func clusterListTool() copilot.Tool {
	return copilot.Tool{
		Name:        "ksail_cluster_list",
		Description: "List all KSail clusters. Use this to see available clusters before operating on them.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"all": map[string]interface{}{
					"type":        "boolean",
					"description": "Include stopped/exited clusters",
				},
			},
		},
		Handler: func(invocation copilot.ToolInvocation) (copilot.ToolResult, error) {
			args := []string{"cluster", "list"}

			if params, ok := invocation.Arguments.(map[string]interface{}); ok {
				if all, ok := params["all"].(bool); ok && all {
					args = append(args, "--all")
				}
			}

			output, err := RunKSailCommand(args...)
			if err != nil {
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf("Error listing clusters: %v\nOutput: %s", err, output),
					ResultType:       "failure",
				}, nil
			}

			return copilot.ToolResult{
				TextResultForLLM: output,
				ResultType:       "success",
			}, nil
		},
	}
}

// clusterInfoTool gets information about a specific cluster.
func clusterInfoTool() copilot.Tool {
	return copilot.Tool{
		Name:        "ksail_cluster_info",
		Description: "Get detailed information about a KSail cluster including nodes, status, and configuration.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the cluster (optional, uses current context if not specified)",
				},
			},
		},
		Handler: func(invocation copilot.ToolInvocation) (copilot.ToolResult, error) {
			args := []string{"cluster", "info"}

			if params, ok := invocation.Arguments.(map[string]interface{}); ok {
				if name, ok := params["name"].(string); ok && name != "" {
					args = append(args, "--name", name)
				}
			}

			output, err := RunKSailCommand(args...)
			if err != nil {
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf("Error getting cluster info: %v\nOutput: %s", err, output),
					ResultType:       "failure",
				}, nil
			}

			return copilot.ToolResult{
				TextResultForLLM: output,
				ResultType:       "success",
			}, nil
		},
	}
}

// workloadGetTool gets Kubernetes resources from the cluster.
func workloadGetTool() copilot.Tool {
	return copilot.Tool{
		Name:        "ksail_workload_get",
		Description: "Get Kubernetes resources from the cluster. Equivalent to 'kubectl get' but using KSail.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"resource": map[string]interface{}{
					"type":        "string",
					"description": "Resource type to get (e.g., pods, deployments, services, nodes)",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Specific resource name (optional)",
				},
				"namespace": map[string]interface{}{
					"type":        "string",
					"description": "Namespace (optional, defaults to default namespace)",
				},
				"all_namespaces": map[string]interface{}{
					"type":        "boolean",
					"description": "Get resources from all namespaces",
				},
				"output": map[string]interface{}{
					"type":        "string",
					"description": "Output format: wide, yaml, json",
					"enum":        []string{"wide", "yaml", "json"},
				},
			},
			"required": []string{"resource"},
		},
		Handler: func(invocation copilot.ToolInvocation) (copilot.ToolResult, error) {
			params, ok := invocation.Arguments.(map[string]interface{})
			if !ok {
				return copilot.ToolResult{
					TextResultForLLM: "Invalid parameters",
					ResultType:       "failure",
				}, nil
			}

			resource, _ := params["resource"].(string)
			if resource == "" {
				return copilot.ToolResult{
					TextResultForLLM: "Resource type is required",
					ResultType:       "failure",
				}, nil
			}

			args := []string{"workload", "get", resource}

			if name, ok := params["name"].(string); ok && name != "" {
				args = append(args, name)
			}

			if ns, ok := params["namespace"].(string); ok && ns != "" {
				args = append(args, "-n", ns)
			}

			if allNs, ok := params["all_namespaces"].(bool); ok && allNs {
				args = append(args, "-A")
			}

			if output, ok := params["output"].(string); ok && output != "" {
				args = append(args, "-o", output)
			}

			output, err := RunKSailCommand(args...)
			if err != nil {
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf("Error getting resources: %v\nOutput: %s", err, output),
					ResultType:       "failure",
				}, nil
			}

			return copilot.ToolResult{
				TextResultForLLM: output,
				ResultType:       "success",
			}, nil
		},
	}
}

// readFileTool reads the contents of a file.
func readFileTool() copilot.Tool {
	return copilot.Tool{
		Name:        "read_file",
		Description: "Read the contents of a file. Useful for examining ksail.yaml, Kubernetes manifests, or other configuration files.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file (relative to current directory or absolute)",
				},
			},
			"required": []string{"path"},
		},
		Handler: func(invocation copilot.ToolInvocation) (copilot.ToolResult, error) {
			params, ok := invocation.Arguments.(map[string]interface{})
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

			// Resolve relative paths
			if !filepath.IsAbs(path) {
				wd, err := os.Getwd()
				if err != nil {
					return copilot.ToolResult{
						TextResultForLLM: fmt.Sprintf("Error getting working directory: %v", err),
						ResultType:       "failure",
					}, nil
				}
				path = filepath.Join(wd, path)
			}

			content, err := os.ReadFile(path)
			if err != nil {
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf("Error reading file: %v", err),
					ResultType:       "failure",
				}, nil
			}

			// Truncate if too large
			maxSize := 50000
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
		Name:        "list_directory",
		Description: "List the contents of a directory. Useful for exploring project structure and finding Kubernetes manifests.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the directory (relative to current directory or absolute)",
				},
			},
			"required": []string{"path"},
		},
		Handler: func(invocation copilot.ToolInvocation) (copilot.ToolResult, error) {
			params, ok := invocation.Arguments.(map[string]interface{})
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

			// Resolve relative paths
			if !filepath.IsAbs(path) {
				wd, err := os.Getwd()
				if err != nil {
					return copilot.ToolResult{
						TextResultForLLM: fmt.Sprintf("Error getting working directory: %v", err),
						ResultType:       "failure",
					}, nil
				}
				path = filepath.Join(wd, path)
			}

			entries, err := os.ReadDir(path)
			if err != nil {
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf("Error listing directory: %v", err),
					ResultType:       "failure",
				}, nil
			}

			var result strings.Builder
			result.WriteString(fmt.Sprintf("Contents of %s:\n", path))
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

// RunKSailCommand runs a ksail command and returns the output.
func RunKSailCommand(args ...string) (string, error) {
	ksailPath := FindKSailExecutable()
	if ksailPath == "" {
		return "", fmt.Errorf("ksail executable not found")
	}

	cmd := exec.Command(ksailPath, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
