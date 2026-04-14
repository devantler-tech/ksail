package chat

import (
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	copilot "github.com/github/copilot-sdk/go"
)

// Slash command message types sent from command handlers to the TUI event channel.

// modeChangeRequestMsg requests a specific chat mode change via slash command.
type modeChangeRequestMsg struct {
	Mode ChatMode
}

// openModelPickerMsg requests the model picker to be opened.
type openModelPickerMsg struct{}

// modelSetRequestMsg requests a specific model change via slash command.
type modelSetRequestMsg struct {
	Model string
}

// openSessionPickerMsg requests the session picker to be opened.
type openSessionPickerMsg struct{}

// newChatRequestMsg requests creation of a new chat session.
type newChatRequestMsg struct{}

// showHelpMsg requests the help overlay to be shown.
type showHelpMsg struct{}

// clearViewportMsg requests the viewport to be cleared.
type clearViewportMsg struct{}

// CommandOption represents a selectable option for a slash command argument.
type CommandOption struct {
	Name        string
	Description string
}

// CommandOptionProvider returns the available options for a command.
// It receives the Model to support dynamic options (e.g., model list).
type CommandOptionProvider func(m *Model) []CommandOption

// ParseChatMode parses a string into a ChatMode.
// Returns the mode and true if valid, or InteractiveMode and false if invalid.
func ParseChatMode(name string) (ChatMode, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "interactive":
		return InteractiveMode, true
	case "plan":
		return PlanMode, true
	case "autopilot":
		return AutopilotMode, true
	default:
		return InteractiveMode, false
	}
}

// BuildTUISlashCommands creates slash command definitions for the TUI chat.
// Each command handler sends a message to the TUI event channel for processing.
func BuildTUISlashCommands(eventChan chan<- tea.Msg) []copilot.CommandDefinition {
	return []copilot.CommandDefinition{
		{
			Name:        "mode",
			Description: "Switch chat mode (interactive, plan, autopilot)",
			Handler: func(ctx copilot.CommandContext) error {
				return handleModeCommand(ctx.Args, eventChan)
			},
		},
		{
			Name:        "model",
			Description: "Switch LLM model (opens picker if no name given)",
			Handler: func(ctx copilot.CommandContext) error {
				return handleModelCommand(ctx.Args, eventChan)
			},
		},
		{
			Name:        "new",
			Description: "Start a new chat session",
			Handler: func(_ copilot.CommandContext) error {
				eventChan <- newChatRequestMsg{}
				return nil
			},
		},
		{
			Name:        "sessions",
			Description: "Open session history picker",
			Handler: func(_ copilot.CommandContext) error {
				eventChan <- openSessionPickerMsg{}
				return nil
			},
		},
		{
			Name:        "help",
			Description: "Show keyboard shortcuts and commands",
			Handler: func(_ copilot.CommandContext) error {
				eventChan <- showHelpMsg{}
				return nil
			},
		},
		{
			Name:        "clear",
			Description: "Clear the chat viewport",
			Handler: func(_ copilot.CommandContext) error {
				eventChan <- clearViewportMsg{}
				return nil
			},
		},
	}
}

// BuildNonTUISlashCommands creates slash command definitions for non-TUI chat mode.
// Commands perform actions directly since there is no TUI event channel.
func BuildNonTUISlashCommands(writer io.Writer) []copilot.CommandDefinition {
	return []copilot.CommandDefinition{
		{
			Name:        "help",
			Description: "Show available commands",
			Handler: func(_ copilot.CommandContext) error {
				_, _ = fmt.Fprintln(writer, "\nAvailable commands:")
				_, _ = fmt.Fprintln(writer, "  /help              Show this help")
				_, _ = fmt.Fprintln(writer, "  /mode <mode>       Switch mode (interactive, plan, autopilot)")
				_, _ = fmt.Fprintln(writer, "  exit, quit, q      Exit the chat")
				_, _ = fmt.Fprintln(writer, "")

				return nil
			},
		},
		{
			Name:        "mode",
			Description: "Switch chat mode (interactive, plan, autopilot)",
			Handler: func(ctx copilot.CommandContext) error {
				args := strings.TrimSpace(ctx.Args)
				if args == "" {
					_, _ = fmt.Fprintln(writer, "Usage: /mode <interactive|plan|autopilot>")
					return nil
				}

				_, ok := ParseChatMode(args)
				if !ok {
					_, _ = fmt.Fprintf(writer, "Invalid mode %q: use interactive, plan, or autopilot\n", args)
					return nil
				}

				_, _ = fmt.Fprintf(writer, "Switched to %s mode\n", args)
				return nil
			},
		},
	}
}

// handleModeCommand parses mode args and sends the appropriate message.
func handleModeCommand(args string, eventChan chan<- tea.Msg) error {
	args = strings.TrimSpace(args)
	if args == "" {
		return fmt.Errorf("usage: /mode <interactive|plan|autopilot>")
	}

	mode, ok := ParseChatMode(args)
	if !ok {
		return fmt.Errorf("invalid mode %q: use interactive, plan, or autopilot", args)
	}

	eventChan <- modeChangeRequestMsg{Mode: mode}

	return nil
}

// handleModelCommand parses model args and sends the appropriate message.
func handleModelCommand(args string, eventChan chan<- tea.Msg) error {
	args = strings.TrimSpace(args)
	if args == "" {
		eventChan <- openModelPickerMsg{}
		return nil
	}

	eventChan <- modelSetRequestMsg{Model: args}

	return nil
}

// BuildTUICommandOptions returns a map of command name → option provider
// for commands that support argument autocompletion.
func BuildTUICommandOptions() map[string]CommandOptionProvider {
	return map[string]CommandOptionProvider{
		"mode": func(_ *Model) []CommandOption {
			return []CommandOption{
				{Name: "interactive", Description: "Confirm each action"},
				{Name: "plan", Description: "Create plans before acting"},
				{Name: "autopilot", Description: "Act autonomously"},
			}
		},
		"model": func(m *Model) []CommandOption {
			if len(m.availableModels) == 0 {
				allModels, err := m.client.ListModels(m.ctx)
				if err == nil {
					m.availableModels = FilterEnabledModels(allModels)
				}
			}

			options := make([]CommandOption, 0, len(m.availableModels))
			for _, model := range m.availableModels {
				options = append(options, CommandOption{
					Name:        model.ID,
					Description: model.Name,
				})
			}

			return options
		},
	}
}
