package chat

import (
	"strings"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/spf13/cobra"
)

// BuildSystemSections builds system prompt section overrides for the "customize" mode.
// It delegates to the generic BuildSystemSectionsFromConfig with KSail-specific defaults.
// The root command supplies the CLI help text in-process.
func BuildSystemSections(rootCmd *cobra.Command) map[string]copilot.SectionOverride {
	return BuildSystemSectionsFromConfig(DefaultSystemContextConfig(rootCmd))
}

// DefaultSystemContextConfig returns the default KSail system context configuration.
// The CLI help section is rendered from the in-process root command instead of
// spawning a ksail subprocess, so it works regardless of PATH or install location.
func DefaultSystemContextConfig(rootCmd *cobra.Command) SystemContextConfig {
	return SystemContextConfig{
		Identity: "You are KSail Assistant, an AI helper for KSail - " +
			"a CLI tool for creating and managing local Kubernetes clusters.\n" +
			"You help users configure, troubleshoot, and operate Kubernetes clusters using KSail.",
		Documentation:            loadDocumentation(),
		CLIHelp:                  rootCommandHelp(rootCmd),
		Instructions:             ksailInstructions,
		IncludeWorkingDirContext: true,
		ConfigFileName:           "ksail.yaml",
	}
}

// rootCommandHelp renders the root command's help text (long description plus
// usage) in-process, matching what `ksail --help` prints.
func rootCommandHelp(rootCmd *cobra.Command) string {
	if rootCmd == nil {
		return ""
	}

	description := strings.TrimSpace(rootCmd.Long)
	if description == "" {
		description = strings.TrimSpace(rootCmd.Short)
	}

	usage := rootCmd.UsageString()
	if description == "" {
		return usage
	}

	return description + "\n\n" + usage
}

// loadDocumentation returns the generated documentation constant.
// The documentation is pre-processed at go:generate time from docs/src/content/docs/.
func loadDocumentation() string {
	return generatedDocumentation
}

// ksailInstructions contains instructions for the AI assistant.
const ksailInstructions = `<instructions>
- Help users configure and manage Kubernetes clusters using KSail
- When suggesting commands, explain what they do before running them
- For write operations (creating clusters, applying workloads, deleting resources),
  the user will be prompted to confirm unless YOLO mode is enabled (Ctrl+Y in TUI)
- ALWAYS use the registered KSail tools (e.g., cluster_write, cluster_read, workload_write with command="apply")
  instead of running ksail commands through bash, shell, or terminal tools.
  The registered tools handle confirmation prompts and force flags automatically.
  Running ksail commands through bash will block on interactive prompts.
- Reference the documentation when helping with ksail.yaml configuration
- Use the troubleshooting tips for diagnosing issues
- When the user reports a cluster problem or asks why something is failing,
  call the cluster_read tool with command="diagnose" to fetch the current
  list of failing pods and NotReady nodes, then explain each failure's
  likely root cause and suggest concrete remediation steps.
- Be concise but thorough in explanations
- If a ksail.yaml exists in the working directory, reference it when relevant
- When generating configuration, follow the ksail.yaml schema
- For cluster operations, verify the cluster exists first with cluster_read using command="list"
- IMPORTANT: Do NOT call the same tool multiple times with the same arguments.
- If a command returns "No clusters found", respond to the user accordingly - do not retry.
- When asked to delete/modify clusters but none exist, inform the user there are no clusters to act on.
</instructions>
`
