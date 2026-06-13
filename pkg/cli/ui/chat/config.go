package chat

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/asciiart"
	chatsvc "github.com/devantler-tech/ksail/v7/pkg/svc/chat"
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

	// ExitMessage is shown as the title in the exit confirmation modal (e.g., "Exit chat?").
	ExitMessage string

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

	// Theme configures colors, logo, labels, and other visual aspects.
	// Zero value applies DefaultThemeConfig().
	Theme ThemeConfig

	// ToolDisplay configures tool name mappings and command builders.
	// Zero value applies DefaultToolDisplayConfig().
	ToolDisplay ToolDisplayConfig
}

// SystemContextConfig configures the system prompt for the AI assistant.
// Use BuildSystemContext() to produce a formatted system context string from this config.
//
// The canonical definition lives in pkg/svc/chat; this alias preserves the
// chatui-facing API while keeping the import direction ui -> svc.
type SystemContextConfig = chatsvc.SystemContextConfig

// Default logo and tagline constants.
const (
	defaultLogoHeight = 6

	defaultTagline = "AI-Powered Kubernetes Assistant"
)

// DefaultThemeConfig returns the default theme configuration with KSail branding.
func DefaultThemeConfig() ThemeConfig {
	return ThemeConfig{
		Logo:           asciiart.Logo,
		Tagline:        func() string { return defaultTagline },
		LogoHeight:     defaultLogoHeight,
		AssistantLabel: "▶ KSail",
		Placeholder:    "Ask me anything about Kubernetes, KSail, or cluster management...",
		WelcomeMessage: "Type a message below to start chatting with KSail AI.",
		ExitMessage:    "Exit chat?",
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
// The implementation lives in pkg/svc/chat; this wrapper preserves the chatui API.
func BuildSystemContext(cfg SystemContextConfig) (string, error) {
	return chatsvc.BuildSystemContext(cfg) //nolint:wrapcheck // thin pass-through to chatsvc
}

// BuildSystemSections builds system prompt section overrides for the "customize" mode.
// This maps the SystemContextConfig fields to SDK section identifiers.
// The implementation lives in pkg/svc/chat; this wrapper preserves the chatui API.
func BuildSystemSections(cfg SystemContextConfig) map[string]copilot.SectionOverride {
	return chatsvc.BuildSystemSectionsFromConfig(cfg)
}
