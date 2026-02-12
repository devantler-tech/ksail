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

	chatui "github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
)

// BuildSystemContext builds the system prompt context for the chat assistant.
// It delegates to the generic chatui.BuildSystemContext with KSail-specific defaults.
func BuildSystemContext() (string, error) {
	result, err := chatui.BuildSystemContext(DefaultSystemContextConfig())
	if err != nil {
		return "", fmt.Errorf("building system context: %w", err)
	}

	return result, nil
}

// DefaultSystemContextConfig returns the default KSail system context configuration.
func DefaultSystemContextConfig() chatui.SystemContextConfig {
	return chatui.SystemContextConfig{
		Identity: "You are KSail Assistant, an AI helper for KSail - " +
			"a CLI tool for creating and managing local Kubernetes clusters.\n" +
			"You help users configure, troubleshoot, and operate Kubernetes clusters using KSail.",
		Documentation:            loadDocumentation(),
		CLIHelp:                  getCLIHelp(),
		Instructions:             ksailInstructions,
		IncludeWorkingDirContext: true,
		ConfigFileName:           "ksail.yaml",
	}
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

// loadDocumentation returns the generated documentation constant.
// The documentation is pre-processed at go:generate time from docs/src/content/docs/.
func loadDocumentation() string {
	return generatedDocumentation
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
- For write operations (creating clusters, applying workloads, deleting resources),
  the user will be prompted to confirm unless YOLO mode is enabled (Ctrl+Y in TUI)
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
