package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	copilot "github.com/github/copilot-sdk/go"
)

// SystemContextConfig configures the system prompt for the AI assistant.
// Use BuildSystemContext() to produce a formatted system context string from this config.
//
// These primitives are chat-core (system-prompt construction) and have no
// dependency on terminal rendering, so they live in the service layer; the TUI
// package re-exports them so the import direction stays ui -> svc.
type SystemContextConfig struct {
	// Identity is the assistant's identity and role description.
	Identity string

	// Documentation is reference documentation embedded in the system prompt.
	Documentation string

	// CLIHelp is pre-rendered CLI help text (e.g., from --help output).
	CLIHelp string

	// Instructions are behavioral instructions for the assistant.
	Instructions string

	// IncludeWorkingDirContext enables auto-detection of current working directory
	// and config file content to include in the prompt.
	IncludeWorkingDirContext bool

	// ConfigFileName is the config file to detect in the working directory (e.g., "ksail.yaml").
	// Only used when IncludeWorkingDirContext is true.
	ConfigFileName string
}

// BuildSystemContext builds a formatted system context string from the given configuration.
// This produces the system prompt passed to the AI assistant session.
func BuildSystemContext(cfg SystemContextConfig) (string, error) {
	var builder strings.Builder

	if cfg.Identity != "" {
		builder.WriteString("<identity>\n")
		builder.WriteString(cfg.Identity)
		builder.WriteString("\n</identity>\n\n")
	}

	if cfg.IncludeWorkingDirContext {
		appendWorkingDirContext(&builder, cfg.ConfigFileName)
	}

	if custom := buildCustomInstructions(cfg); custom != "" {
		builder.WriteString(custom)
	}

	return builder.String(), nil
}

// BuildSystemSectionsFromConfig builds system prompt section overrides for the
// "customize" mode. This maps the SystemContextConfig fields to SDK section
// identifiers. The KSail-specific BuildSystemSections (which takes the root
// command) delegates to this.
func BuildSystemSectionsFromConfig(cfg SystemContextConfig) map[string]copilot.SectionOverride {
	sections := make(map[string]copilot.SectionOverride)

	if cfg.Identity != "" {
		sections[copilot.SectionIdentity] = copilot.SectionOverride{
			Action:  copilot.SectionActionReplace,
			Content: cfg.Identity,
		}
	}

	if envCtx := buildEnvironmentContext(cfg); envCtx != "" {
		sections[copilot.SectionEnvironmentContext] = copilot.SectionOverride{
			Action:  copilot.SectionActionAppend,
			Content: envCtx,
		}
	}

	if customCtx := buildCustomInstructions(cfg); customCtx != "" {
		sections[copilot.SectionCustomInstructions] = copilot.SectionOverride{
			Action:  copilot.SectionActionAppend,
			Content: customCtx,
		}
	}

	return sections
}

// buildEnvironmentContext builds the environment context content (working directory + config file).
func buildEnvironmentContext(cfg SystemContextConfig) string {
	if !cfg.IncludeWorkingDirContext {
		return ""
	}

	var builder strings.Builder
	appendWorkingDirContext(&builder, cfg.ConfigFileName)

	return builder.String()
}

// buildCustomInstructions builds the custom instructions content (documentation + CLI help + instructions).
func buildCustomInstructions(cfg SystemContextConfig) string {
	var builder strings.Builder

	if cfg.Documentation != "" {
		builder.WriteString("<documentation>\n")
		builder.WriteString(cfg.Documentation)
		builder.WriteString("\n</documentation>\n\n")
	}

	if cfg.CLIHelp != "" {
		builder.WriteString("<cli_help>\n")
		builder.WriteString(cfg.CLIHelp)
		builder.WriteString("\n</cli_help>\n\n")
	}

	if cfg.Instructions != "" {
		builder.WriteString(cfg.Instructions)
	}

	return builder.String()
}

// appendWorkingDirContext adds working directory and config file context to the builder.
func appendWorkingDirContext(builder *strings.Builder, configFileName string) {
	workDir, err := os.Getwd()
	if err != nil {
		return
	}

	fmt.Fprintf(builder, "<working_directory>%s</working_directory>\n\n", workDir)

	if configFileName == "" {
		return
	}

	configPath := filepath.Join(workDir, configFileName)

	content, readErr := os.ReadFile(configPath) //nolint:gosec // Reading local config file is safe
	if readErr != nil {
		return
	}

	builder.WriteString("<current_config>\n")
	builder.Write(content)
	builder.WriteString("\n</current_config>\n\n")
}

// IsReadOperation determines if a permission request is for a read-only operation.
// This is shared by both TUI and non-TUI permission handlers.
func IsReadOperation(kind copilot.PermissionRequestKind) bool {
	switch kind {
	case copilot.PermissionRequestKindRead, copilot.PermissionRequestKindURL:
		return true
	case copilot.PermissionRequestKindCustomTool,
		copilot.PermissionRequestKindShell,
		copilot.PermissionRequestKindMcp,
		copilot.PermissionRequestKindMemory,
		copilot.PermissionRequestKindWrite,
		copilot.PermissionRequestKindHook:
		return false
	default:
		return false
	}
}
