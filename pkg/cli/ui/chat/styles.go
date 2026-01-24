package chat

import "github.com/charmbracelet/lipgloss"

// ASCII art logo for the header.
const logo = `  ██╗  ██╗███████╗ █████╗ ██╗██╗
  ██║ ██╔╝██╔════╝██╔══██╗██║██║
  █████╔╝ ███████╗███████║██║██║
  ██╔═██╗ ╚════██║██╔══██║██║██║
  ██║  ██╗███████║██║  ██║██║███████╗
  ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═╝╚══════╝`

// logoHeight is the number of lines in the logo.
const logoHeight = 6

// tagline displayed under the logo.
const tagline = "AI-Powered Kubernetes Assistant"

var (
	// Color palette - ocean/nautical theme.
	primaryColor   = lipgloss.Color("39")  // Bright cyan (ocean)
	accentColor    = lipgloss.Color("45")  // Lighter cyan
	secondaryColor = lipgloss.Color("245") // Gray
	userColor      = lipgloss.Color("117") // Sky blue
	assistantColor = lipgloss.Color("183") // Soft purple
	toolColor      = lipgloss.Color("215") // Warm orange
	successColor   = lipgloss.Color("78")  // Green
	dimColor       = lipgloss.Color("240") // Dim gray

	// logoStyle renders the ASCII art logo.
	logoStyle = lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true)

	// taglineStyle renders the tagline under the logo.
	taglineStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Italic(true).
			MarginBottom(1)

	// headerBoxStyle wraps the entire header section.
	headerBoxStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(0, 2).
			MarginBottom(1)

	// userMsgStyle is the style for user messages.
	userMsgStyle = lipgloss.NewStyle().
			Foreground(userColor).
			Bold(true)

	// assistantMsgStyle is the style for assistant message labels.
	assistantMsgStyle = lipgloss.NewStyle().
				Foreground(assistantColor).
				Bold(true)

	// toolMsgStyle is the style for tool call/result messages.
	toolMsgStyle = lipgloss.NewStyle().
			Foreground(toolColor)

	// toolSuccessStyle is the style for completed tool messages.
	toolSuccessStyle = lipgloss.NewStyle().
				Foreground(successColor)

	// toolOutputStyle is the style for tool output text.
	toolOutputStyle = lipgloss.NewStyle().
			Foreground(dimColor)

	// helpStyle is the style for help text.
	helpStyle = lipgloss.NewStyle().
			Foreground(dimColor).
			MarginTop(1)

	// spinnerStyle is the style for the loading spinner.
	spinnerStyle = lipgloss.NewStyle().
			Foreground(accentColor)

	// viewportStyle styles the chat area.
	viewportStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(secondaryColor).
			Padding(0, 1)

	// inputStyle styles the input textarea.
	inputStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(0, 1)

	// statusStyle is for status messages.
	statusStyle = lipgloss.NewStyle().
			Foreground(dimColor).
			Italic(true)

	// warningColor for permission prompts.
	warningColor = lipgloss.Color("214") // Amber/warning

	// permissionBoxStyle styles the permission dialog.
	permissionBoxStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(warningColor).
				Padding(1, 2).
				MarginTop(1).
				MarginBottom(1)

	// permissionTitleStyle styles the permission dialog title.
	permissionTitleStyle = lipgloss.NewStyle().
				Foreground(warningColor).
				Bold(true)

	// permissionDescStyle styles the permission description text.
	permissionDescStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))
)
