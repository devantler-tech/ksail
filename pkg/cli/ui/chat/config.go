package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	copilot "github.com/github/copilot-sdk/go"
)

// ThemeConfig configures the visual appearance and branding of the chat TUI.
// All fields have sensible defaults — use DefaultThemeConfig() as a starting point
// and override the fields you need.
type ThemeConfig struct {
	// Logo returns the ASCII art logo text (multi-line). Called on each render.
	Logo func() string

	// Tagline returns the tagline text displayed below the logo.
	Tagline func() string

	// LogoHeight is the number of lines in the logo (used for layout calculations).
	LogoHeight int

	// AssistantLabel is the label shown before assistant messages (e.g., "▶ KSail").
	AssistantLabel string

	// Placeholder is the textarea placeholder text.
	Placeholder string

	// WelcomeMessage is shown in the viewport before any messages.
	WelcomeMessage string

	// GoodbyeMessage is shown when the user quits.
	GoodbyeMessage string

	// SessionDir is the app-specific directory name for session storage under $HOME.
	// For example, ".ksail" stores sessions at ~/.ksail/chat/sessions.
	SessionDir string

	// Color palette — uses AdaptiveColor for automatic light/dark theme support.

	// PrimaryColor is used for main accents (header border, input border, logo).
	PrimaryColor lipgloss.AdaptiveColor

	// AccentColor is used for tagline and spinner.
	AccentColor lipgloss.AdaptiveColor

	// SecondaryColor is used for borders and muted elements.
	SecondaryColor lipgloss.AdaptiveColor

	// UserColor styles user messages.
	UserColor lipgloss.AdaptiveColor

	// AssistantColor styles assistant message labels.
	AssistantColor lipgloss.AdaptiveColor

	// ToolColor styles tool calls and help keybindings.
	ToolColor lipgloss.AdaptiveColor

	// SuccessColor styles success indicators and completed tools.
	SuccessColor lipgloss.AdaptiveColor

	// DimColor styles muted/less important text.
	DimColor lipgloss.AdaptiveColor

	// ErrorColor styles error messages.
	ErrorColor lipgloss.AdaptiveColor
}

// ToolDisplayConfig configures how tool names and commands are displayed in the TUI.
type ToolDisplayConfig struct {
	// NameMappings maps tool names to human-readable labels.
	// Example: {"my_tool_list": "Listing items", "bash": "Running command"}
	// The generic fallback (snake_case → Title Case) is used for unmapped names.
	NameMappings map[string]string

	// CommandBuilders maps tool names to functions that extract display commands from arguments.
	CommandBuilders map[string]CommandBuilder
}

// CommandBuilder extracts a display command string from tool arguments for display in the TUI.
type CommandBuilder func(args map[string]any) string

// Params bundles the parameters for creating and running the chat TUI.
// Use this struct with NewModel or Run to configure the chat session.
type Params struct {
	// Session is the active Copilot chat session.
	Session *copilot.Session

	// Client is the Copilot client used for API calls.
	Client *copilot.Client

	// SessionConfig holds the session configuration.
	SessionConfig *copilot.SessionConfig

	// Models is the list of available models.
	Models []copilot.ModelInfo

	// CurrentModel is the initially-selected model identifier.
	CurrentModel string

	// Timeout controls the per-request timeout.
	Timeout time.Duration

	// EventChan is an optional pre-created event channel for sending external events to the TUI.
	// If nil, a new channel is created internally.
	EventChan chan tea.Msg

	// ChatModeRef is an optional reference to synchronize chat mode state with tool handlers.
	ChatModeRef *ChatModeRef

	// YoloModeRef is an optional reference to synchronize YOLO mode state with tool handlers.
	YoloModeRef *YoloModeRef

	// Theme configures colors, logo, labels, and other visual aspects.
	// Zero value applies DefaultThemeConfig().
	Theme ThemeConfig

	// ToolDisplay configures tool name mappings and command builders.
	// Zero value applies DefaultToolDisplayConfig().
	ToolDisplay ToolDisplayConfig
}

// SystemContextConfig configures the system prompt for the AI assistant.
// Use BuildSystemContext() to produce a formatted system context string from this config.
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

// Default logo and tagline constants.
const (
	defaultLogoHeight = 6

	defaultLogo = "██╗  ██╗███████╗ █████╗ ██╗██╗\n" +
		"██║ ██╔╝██╔════╝██╔══██╗██║██║\n" +
		"█████╔╝ ███████╗███████║██║██║\n" +
		"██╔═██╗ ╚════██║██╔══██║██║██║\n" +
		"██║  ██╗███████║██║  ██║██║███████╗\n" +
		"╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═╝╚══════╝"

	defaultTagline = "AI-Powered Kubernetes Assistant"
)

// DefaultThemeConfig returns the default theme configuration with KSail branding.
func DefaultThemeConfig() ThemeConfig {
	return ThemeConfig{
		Logo:           func() string { return defaultLogo },
		Tagline:        func() string { return defaultTagline },
		LogoHeight:     defaultLogoHeight,
		AssistantLabel: "▶ KSail",
		Placeholder:    "Ask me anything about Kubernetes, KSail, or cluster management...",
		WelcomeMessage: "Type a message below to start chatting with KSail AI.",
		GoodbyeMessage: "Goodbye! Thanks for using KSail.",
		SessionDir:     ".ksail",
		PrimaryColor: lipgloss.AdaptiveColor{
			Light: "#0891b2",
			Dark:  "#22d3ee",
		}, // cyan-600 / cyan-400
		AccentColor: lipgloss.AdaptiveColor{
			Light: "#0e7490",
			Dark:  "#67e8f9",
		}, // cyan-700 / cyan-300
		SecondaryColor: lipgloss.AdaptiveColor{
			Light: "#6b7280",
			Dark:  "#9ca3af",
		}, // gray-500 / gray-400
		UserColor: lipgloss.AdaptiveColor{
			Light: "#2563eb",
			Dark:  "#60a5fa",
		}, // blue-600 / blue-400
		AssistantColor: lipgloss.AdaptiveColor{
			Light: "#9333ea",
			Dark:  "#c084fc",
		}, // purple-600 / purple-400
		ToolColor: lipgloss.AdaptiveColor{
			Light: "#d97706",
			Dark:  "#fbbf24",
		}, // amber-600 / amber-400
		SuccessColor: lipgloss.AdaptiveColor{
			Light: "#16a34a",
			Dark:  "#4ade80",
		}, // green-600 / green-400
		DimColor: lipgloss.AdaptiveColor{
			Light: "#9ca3af",
			Dark:  "#6b7280",
		}, // gray-400 / gray-500
		ErrorColor: lipgloss.AdaptiveColor{
			Light: "#dc2626",
			Dark:  "#f87171",
		}, // red-600 / red-400
	}
}

// DefaultToolDisplayConfig returns the default tool display configuration with KSail tool mappings.
func DefaultToolDisplayConfig() ToolDisplayConfig {
	return ToolDisplayConfig{
		NameMappings: map[string]string{
			"report_intent":        "Analyzing request",
			"ksail_cluster_list":   "Listing clusters",
			"ksail_cluster_info":   "Getting cluster info",
			"ksail_cluster_create": "Creating cluster",
			"ksail_cluster_delete": "Deleting cluster",
			"bash":                 "Running command",
			"read_file":            "Reading file",
			"write_file":           "Writing file",
			"list_dir":             "Listing directory",
			"list_directory":       "Listing directory",
		},
		CommandBuilders: defaultCommandBuilders(),
	}
}

// defaultCommandBuilders returns the default KSail command builders.
func defaultCommandBuilders() map[string]CommandBuilder {
	return map[string]CommandBuilder{
		"ksail_cluster_list": defaultBuildClusterListCommand,
		"ksail_cluster_info": defaultBuildClusterInfoCommand,
		"ksail_workload_get": defaultBuildWorkloadGetCommand,
		"read_file":          defaultBuildReadFileCommand,
		"list_dir":           defaultBuildListDirectoryCommand,
		"list_directory":     defaultBuildListDirectoryCommand,
	}
}

func defaultBuildClusterListCommand(args map[string]any) string {
	cmd := "ksail cluster list"

	if all, ok := args["all"].(bool); ok && all {
		cmd += " --all"
	}

	return cmd
}

func defaultBuildClusterInfoCommand(args map[string]any) string {
	cmd := "ksail cluster info"

	if name, ok := args["name"].(string); ok && name != "" {
		cmd += " --name " + name
	}

	return cmd
}

func defaultBuildWorkloadGetCommand(args map[string]any) string {
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

func defaultBuildReadFileCommand(args map[string]any) string {
	if path, ok := args["path"].(string); ok && path != "" {
		return "cat " + path
	}

	return ""
}

func defaultBuildListDirectoryCommand(args map[string]any) string {
	path := "."

	if p, ok := args["path"].(string); ok && p != "" {
		path = p
	}

	return "ls " + path
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

	return builder.String(), nil
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
