package chat

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	copilot "github.com/github/copilot-sdk/go"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// handlePermissionRequest handles incoming permission request messages.
func (m *Model) handlePermissionRequest(req *permissionRequestMsg) (tea.Model, tea.Cmd) {
	m.pendingPermission = req
	m.updateDimensions()
	m.updateViewportContent()

	return m, nil
}

// handlePermissionKey handles keyboard input when a permission prompt is active.
func (m *Model) handlePermissionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pendingPermission == nil {
		return m, nil
	}

	switch msg.String() {
	case "y", "Y":
		return m.allowPermission()
	case "n", "N", "esc":
		return m.denyPermission()
	case "ctrl+c":
		m.pendingPermission.response <- false

		m.pendingPermission = nil
		m.cleanup()
		m.quitting = true

		return m, tea.Quit
	}

	return m, nil
}

// allowPermission approves the pending permission request.
func (m *Model) allowPermission() (tea.Model, tea.Cmd) {
	m.permissionHistory = append(m.permissionHistory, permissionResponse{
		toolName: m.pendingPermission.toolName,
		command:  m.pendingPermission.command,
		allowed:  true,
	})
	m.pendingPermission.response <- true

	m.pendingPermission = nil
	m.updateDimensions()
	m.updateViewportContent()

	return m, m.waitForEvent()
}

// denyPermission denies the pending permission request.
func (m *Model) denyPermission() (tea.Model, tea.Cmd) {
	m.permissionHistory = append(m.permissionHistory, permissionResponse{
		toolName: m.pendingPermission.toolName,
		command:  m.pendingPermission.command,
		allowed:  false,
	})
	m.pendingPermission.response <- false

	m.pendingPermission = nil
	m.updateDimensions()
	m.updateViewportContent()

	return m, m.waitForEvent()
}

// renderPermissionModal renders the permission prompt as an inline modal section.
func (m *Model) renderPermissionModal() string {
	if m.pendingPermission == nil {
		return ""
	}

	modalWidth := max(m.width-modalPadding, 1)
	mStyles := newModalContentStyles(modalWidth)

	var content strings.Builder

	contentLines := 0

	content.WriteString(
		mStyles.clipStyle.Render(mStyles.warningStyle.Render("⚠️  Permission Required")) + "\n\n",
	)

	contentLines += 2

	humanName := humanizeToolName(m.pendingPermission.toolName, m.toolDisplay.NameMappings)
	content.WriteString(mStyles.clipStyle.Render("Tool: "+humanName) + "\n")

	contentLines++

	if m.pendingPermission.command != "" {
		content.WriteString(
			mStyles.clipStyle.Render("Command: "+m.pendingPermission.command) + "\n",
		)

		contentLines++
	}

	if m.pendingPermission.arguments != "" {
		content.WriteString(
			mStyles.clipStyle.Render("Arguments: "+m.pendingPermission.arguments) + "\n",
		)

		contentLines++
	}

	content.WriteString("\n" + mStyles.clipStyle.Render("Allow this operation?") + "\n")

	contentLines += 3

	modalStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.ANSIColor(ansiYellow)).
		PaddingLeft(1).
		PaddingRight(1).
		Width(modalWidth).
		Height(contentLines)

	return modalStyle.Render(strings.TrimRight(content.String(), "\n"))
}

// CreateTUIPermissionHandler creates a permission handler that integrates with the TUI.
// It sends permission requests to the provided event channel and waits for a response.
// This allows the TUI to display permission prompts and collect user input.
// Read and URL operations are auto-approved to avoid excessive prompting.
// When yoloModeRef is provided and YOLO mode is enabled, permissions are auto-approved.
func CreateTUIPermissionHandler(
	eventChan chan<- tea.Msg,
	yoloModeRef *YoloModeRef,
) copilot.PermissionHandlerFunc {
	return func(
		request copilot.PermissionRequest,
		_ copilot.PermissionInvocation,
	) (copilot.PermissionRequestResult, error) {
		// In YOLO mode, auto-approve all SDK permission requests
		if yoloModeRef != nil && yoloModeRef.IsEnabled() {
			return copilot.PermissionRequestResult{
				Kind: copilot.PermissionRequestResultKindApproved,
			}, nil
		}

		// Auto-approve read operations to avoid excessive prompting.
		// Matches the non-TUI handler's behavior.
		if isReadOperation(request.Kind) {
			return copilot.PermissionRequestResult{
				Kind: copilot.PermissionRequestResultKindApproved,
			}, nil
		}

		// Extract tool name and command from the permission request.
		// The PermissionRequest now has typed fields for all permission details.
		toolName, command := extractPermissionDetails(request)

		// Create response channel
		responseChan := make(chan bool, 1)

		// Send permission request to TUI
		toolCallID := ""
		if request.ToolCallID != nil {
			toolCallID = *request.ToolCallID
		}

		eventChan <- permissionRequestMsg{
			toolCallID: toolCallID,
			toolName:   toolName,
			command:    command,
			arguments:  "", // Arguments are included in the command for SDK permissions
			response:   responseChan,
		}

		// Wait for response from TUI
		approved := <-responseChan

		if approved {
			return copilot.PermissionRequestResult{
				Kind: copilot.PermissionRequestResultKindApproved,
			}, nil
		}

		return copilot.PermissionRequestResult{
			Kind: copilot.PermissionRequestResultKindDeniedInteractivelyByUser,
		}, nil
	}
}

// extractPermissionDetails extracts human-readable tool name and command
// from an SDK permission request.
// The PermissionRequest has typed fields for each permission kind.
func extractPermissionDetails(request copilot.PermissionRequest) (string, string) {
	toolName := formatPermissionKind(request.Kind)

	if detail := firstNonEmpty(
		request.FullCommandText,
		request.FileName,
		request.Path,
		request.ToolName,
		request.URL,
		request.Diff,
	); detail != "" {
		return toolName, detail
	}

	return toolName, string(request.Kind)
}

// firstNonEmpty returns the first non-nil, non-empty string from the given pointers.
func firstNonEmpty(ptrs ...*string) string {
	for _, p := range ptrs {
		if p != nil && *p != "" {
			return *p
		}
	}

	return ""
}

// formatPermissionKind converts a permission kind to a human-readable tool name.
//
//nolint:cyclop // exhaustive type-switch over permission kinds
func formatPermissionKind(kind copilot.PermissionRequestKind) string {
	switch kind {
	case copilot.PermissionRequestKindShell:
		return "Shell Command"
	case copilot.PermissionRequestKindWrite:
		return "File Write"
	case copilot.PermissionRequestKindRead:
		return "File Read"
	case copilot.PermissionRequestKindURL:
		return "URL"
	case copilot.PermissionRequestKindMcp:
		return "MCP Tool"
	case copilot.PermissionRequestKindCustomTool:
		return "Custom Tool"
	case copilot.PermissionRequestKindMemory:
		return "Memory"
	case copilot.PermissionRequestKindHook:
		return "Hook"
	default:
		// Capitalize and format the kind
		kindStr := string(kind)
		if kindStr == "" {
			return unknownOperation
		}
		// Replace underscores with spaces and title case.
		// English titlecase is appropriate for all SDK permission kinds (shell, file_edit, etc.)
		formatted := strings.ReplaceAll(kindStr, "_", " ")
		caser := cases.Title(language.English)

		return caser.String(formatted)
	}
}

// isReadOperation determines if a permission request is for a read-only operation.
func isReadOperation(kind copilot.PermissionRequestKind) bool {
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
