package chat

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"
)

// BuildSystemContext builds the system prompt context for the chat assistant.
// It combines multiple sources of information:
//   - Identity and role description for the AI assistant
//   - Current working directory and ksail.yaml configuration if present
//   - KSail documentation loaded from the docs/ directory
//   - Dynamic CLI help output from the ksail executable
//   - Instructions for proper behavior and command usage
//
// Returns the complete system context string and any error encountered.
// If documentation loading fails, a partial context is still returned.
func BuildSystemContext() (string, error) {
	var builder strings.Builder

	// Add identity and role
	builder.WriteString(`<identity>
You are KSail Assistant, an AI helper for KSail - a CLI tool for creating and managing local Kubernetes clusters.
You help users configure, troubleshoot, and operate Kubernetes clusters using KSail.
</identity>

`)

	// Add working directory context
	workDir, err := os.Getwd()
	if err == nil {
		builder.WriteString(fmt.Sprintf("<working_directory>%s</working_directory>\n\n", workDir))

		// Check if ksail.yaml exists in working directory
		configPath := filepath.Join(workDir, "ksail.yaml")

		_, statErr := os.Stat(configPath)
		if statErr == nil {
			//nolint:gosec // Reading local ksail.yaml config is safe
			content, readErr := os.ReadFile(configPath)
			if readErr == nil {
				builder.WriteString("<current_ksail_config>\n")
				builder.Write(content)
				builder.WriteString("\n</current_ksail_config>\n\n")
			}
		}
	}

	// Load documentation from docs/ directory
	docs := loadDocumentation()
	if docs != "" {
		builder.WriteString("<ksail_documentation>\n")
		builder.WriteString(docs)
		builder.WriteString("\n</ksail_documentation>\n\n")
	}

	// Add CLI help dynamically
	cliHelp := getCLIHelp()
	if cliHelp != "" {
		builder.WriteString("<cli_help>\n")
		builder.WriteString(cliHelp)
		builder.WriteString("\n</cli_help>\n\n")
	}

	// Add instructions
	builder.WriteString(ksailInstructions)

	return builder.String(), nil
}

// getCLIHelp captures the ksail --help output dynamically.
func getCLIHelp() string {
	ksailPath := FindKSailExecutable()
	if ksailPath == "" {
		return ""
	}

	const cliHelpTimeout = 10 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), cliHelpTimeout)
	defer cancel()

	//nolint:gosec // Running own ksail binary with fixed args is safe
	cmd := exec.CommandContext(ctx, ksailPath, "--help")

	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return string(output)
}

// loadDocumentation returns the embedded documentation.
// The documentation is embedded at compile time from the docs/ directory.
func loadDocumentation() string {
	return embeddedDocumentation
}

// FindKSailExecutable attempts to find the ksail executable.
func FindKSailExecutable() string {
	// Check if running from ksail itself
	exe, err := os.Executable()
	if err == nil && strings.Contains(filepath.Base(exe), "ksail") {
		return exe
	}

	// Check PATH
	path, err := exec.LookPath("ksail")
	if err == nil {
		return path
	}

	// Check common locations on macOS/Linux
	if goruntime.GOOS != "windows" {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			commonPaths := []string{
				filepath.Join(homeDir, "go", "bin", "ksail"),
				filepath.Join(homeDir, ".local", "bin", "ksail"),
				"/usr/local/bin/ksail",
			}
			for _, p := range commonPaths {
				_, statErr := os.Stat(p)
				if statErr == nil {
					return p
				}
			}
		}
	}

	return ""
}

// ksailInstructions contains instructions for the AI assistant.
const ksailInstructions = `<instructions>
- Help users configure and manage Kubernetes clusters using KSail
- When suggesting commands, explain what they do before running them
- For write operations (creating clusters, applying workloads, deleting resources), the user will be prompted to confirm
- ALWAYS use the registered KSail tools (e.g., cluster_write, cluster_read, workload_apply)
  instead of running ksail commands through bash, shell, or terminal tools.
  The registered tools handle confirmation prompts and force flags automatically.
  Running ksail commands through bash will block on interactive prompts.
- Reference the documentation when helping with ksail.yaml configuration
- Use the troubleshooting tips for diagnosing issues
- Be concise but thorough in explanations
- If a ksail.yaml exists in the working directory, reference it when relevant
- When generating configuration, follow the ksail.yaml schema
- For cluster operations, verify the cluster exists first with 'ksail cluster list'
- IMPORTANT: Do NOT call the same tool multiple times with the same arguments.
- If a command returns "No clusters found", respond to the user accordingly - do not retry.
- When asked to delete/modify clusters but none exist, inform the user there are no clusters to act on.
</instructions>
`
