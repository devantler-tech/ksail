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

	modalWidth := m.width - modalPadding
	contentWidth := max(modalWidth-contentPadding, 1)
	clipStyle := lipgloss.NewStyle().MaxWidth(contentWidth).Inline(true)
	warningStyle := lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(ansiYellow))

	var content strings.Builder

	contentLines := 0

	content.WriteString(clipStyle.Render(warningStyle.Render("⚠️  Permission Required")) + "\n\n")

	contentLines += 2

	humanName := humanizeToolName(m.pendingPermission.toolName)
	content.WriteString(clipStyle.Render("Tool: "+humanName) + "\n")

	contentLines++

	if m.pendingPermission.command != "" {
		content.WriteString(
			clipStyle.Render("Command: "+m.pendingPermission.command) + "\n",
		)

		contentLines++
	}

	if m.pendingPermission.arguments != "" {
		content.WriteString(
			clipStyle.Render("Arguments: "+m.pendingPermission.arguments) + "\n",
		)

		contentLines++
	}

	content.WriteString("\n" + clipStyle.Render("Allow this operation?") + "\n")

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
func CreateTUIPermissionHandler(eventChan chan<- tea.Msg) copilot.PermissionHandler {
	return func(
		request copilot.PermissionRequest,
		_ copilot.PermissionInvocation,
	) (copilot.PermissionRequestResult, error) {
		// Extract tool name and command from the permission request.
		// The Extra map contains the raw permission request data from the SDK.
		// Common keys vary by permission kind (shell, file_edit, etc.)
		toolName, command := extractPermissionDetails(request)

		// Create response channel
		responseChan := make(chan bool, 1)

		// Send permission request to TUI
		eventChan <- permissionRequestMsg{
			toolCallID: request.ToolCallID,
			toolName:   toolName,
			command:    command,
			arguments:  "", // Arguments are included in the command for SDK permissions
			response:   responseChan,
		}

		// Wait for response from TUI
		approved := <-responseChan

		if approved {
			return copilot.PermissionRequestResult{Kind: "approved"}, nil
		}

		return copilot.PermissionRequestResult{Kind: "denied-interactively-by-user"}, nil
	}
}

// extractPermissionDetails extracts human-readable tool name and command from an SDK permission request.
// The Extra map contains different fields depending on the permission kind.
// Uses priority-based field checking with early returns to avoid deep nesting.
func extractPermissionDetails(request copilot.PermissionRequest) (string, string) {
	// Default tool name based on permission kind
	toolName := formatPermissionKind(request.Kind)

	// Try command fields first, then nested execution, then path fields, then fallback
	if cmd := findCommandInMap(request.Extra); cmd != "" {
		return toolName, cmd
	}

	if cmd := findCommandInExecution(request.Extra); cmd != "" {
		return toolName, cmd
	}

	if path := findPathInMap(request.Extra); path != "" {
		return toolName, path
	}

	if cmd := findFallbackValue(request.Extra); cmd != "" {
		return toolName, cmd
	}

	// Last resort: just show the permission kind
	return toolName, request.Kind
}

// commandFields contains field names that typically hold the command to execute.
var commandFields = []string{
	"command", "cmd", "shell", "runInTerminal", "execute", "run", "script", "input",
}

// pathFields contains field names for file path operations.
var pathFields = []string{"path", "filePath", "file", "target"}

// metadataFields contains field names to skip during fallback search.
var metadataFields = map[string]bool{
	"kind": true, "toolCallId": true, "possiblePaths": true,
	"possibleUrls": true, "sessionId": true, "requestId": true,
	"timestamp": true, "type": true, "id": true,
}

// findCommandInMap searches for a command value in the given map.
func findCommandInMap(extra map[string]any) string {
	for _, field := range commandFields {
		if val, ok := extra[field]; ok {
			if cmd := extractStringValue(val); cmd != "" {
				return cmd
			}
		}
	}

	return ""
}

// findCommandInExecution checks for a nested execution object with command fields.
func findCommandInExecution(extra map[string]any) string {
	exec, ok := extra["execution"].(map[string]any)
	if !ok {
		return ""
	}

	return findCommandInMap(exec)
}

// findPathInMap searches for a path value in the given map.
func findPathInMap(extra map[string]any) string {
	for _, field := range pathFields {
		if val, ok := extra[field]; ok {
			if path := extractStringValue(val); path != "" {
				return path
			}
		}
	}

	return ""
}

// findFallbackValue searches for any non-metadata string value in the given map.
func findFallbackValue(extra map[string]any) string {
	for key, val := range extra {
		if metadataFields[key] {
			continue
		}

		if cmd := extractStringValue(val); cmd != "" {
			return cmd
		}
	}

	return ""
}

// extractStringValue extracts a string from various value types.
func extractStringValue(val any) string {
	switch typedVal := val.(type) {
	case string:
		return typedVal
	case []any:
		// Join array elements
		parts := make([]string, 0, len(typedVal))
		for _, item := range typedVal {
			if s, ok := item.(string); ok && s != "" {
				parts = append(parts, s)
			}
		}

		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	case map[string]any:
		// Try to extract command from nested object
		if cmd, ok := typedVal["command"].(string); ok {
			return cmd
		}

		if cmd, ok := typedVal["cmd"].(string); ok {
			return cmd
		}
	}

	return ""
}

// formatPermissionKind converts a permission kind to a human-readable tool name.
func formatPermissionKind(kind string) string {
	switch kind {
	case "shell":
		return "Shell Command"
	case "file_edit", "fileEdit":
		return "File Edit"
	case "file_read", "fileRead":
		return "File Read"
	case "file_write", "fileWrite":
		return "File Write"
	case "terminal":
		return "Terminal"
	case "browser":
		return "Browser"
	case "network":
		return "Network Request"
	default:
		// Capitalize and format the kind
		if kind == "" {
			return unknownOperation
		}
		// Replace underscores with spaces and title case.
		// English titlecase is appropriate for all SDK permission kinds (shell, file_edit, etc.)
		formatted := strings.ReplaceAll(kind, "_", " ")
		caser := cases.Title(language.English)

		return caser.String(formatted)
	}
}
