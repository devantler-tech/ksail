package chat

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	copilot "github.com/github/copilot-sdk/go"
)

// elicitationRequestMsg carries an elicitation request from the SDK to the TUI.
type elicitationRequestMsg struct {
	Message  string
	Source   string
	Mode     string
	URL      string
	Schema   map[string]any
	Response chan<- elicitationResponsePayload
}

// elicitationResponsePayload is the result sent back from the TUI to the SDK handler.
type elicitationResponsePayload struct {
	Result copilot.ElicitationResult
}

// pendingElicitation tracks the active elicitation dialog state in the TUI.
type pendingElicitation struct {
	request     *elicitationRequestMsg
	inputValue  string   // current text input value (for single-field forms)
	fields      []string // extracted field names from schema
	fieldValues map[string]string
	fieldIndex  int // index into fields for multi-field navigation
}

// handleElicitationRequest handles an incoming elicitation request message.
func (m *Model) handleElicitationRequest(msg elicitationRequestMsg) (tea.Model, tea.Cmd) {
	fields := extractFieldNames(msg.Schema)
	fieldValues := make(map[string]string, len(fields))

	m.pendingElicitation = &pendingElicitation{
		request:     &msg,
		fields:      fields,
		fieldValues: fieldValues,
	}

	m.updateDimensions()
	m.updateViewportContent()

	return m, nil
}

// handleElicitationKey handles keyboard input when an elicitation prompt is active.
func (m *Model) handleElicitationKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pendingElicitation == nil {
		return m, nil
	}

	pe := m.pendingElicitation

	switch msg.String() {
	case "enter":
		return m.acceptElicitation()
	case "esc":
		return m.declineElicitation()
	case "ctrl+c":
		m.cancelElicitation()
		m.cleanup()
		m.quitting = true

		return m, tea.Quit
	case "tab":
		if len(pe.fields) > 1 {
			// Save current field value before switching
			pe.fieldValues[pe.fields[pe.fieldIndex]] = pe.inputValue
			pe.fieldIndex = (pe.fieldIndex + 1) % len(pe.fields)
			pe.inputValue = pe.fieldValues[pe.fields[pe.fieldIndex]]
		}

		return m, nil
	case "shift+tab":
		if len(pe.fields) > 1 {
			pe.fieldValues[pe.fields[pe.fieldIndex]] = pe.inputValue
			pe.fieldIndex = (pe.fieldIndex + len(pe.fields) - 1) % len(pe.fields)
			pe.inputValue = pe.fieldValues[pe.fields[pe.fieldIndex]]
		}

		return m, nil
	case "backspace":
		if len(pe.inputValue) > 0 {
			pe.inputValue = pe.inputValue[:len(pe.inputValue)-1]
		}

		return m, nil
	default:
		if len(msg.String()) == 1 {
			pe.inputValue += msg.String()
		}

		return m, nil
	}
}

// acceptElicitation submits the elicitation with the current input values.
func (m *Model) acceptElicitation() (tea.Model, tea.Cmd) {
	pe := m.pendingElicitation
	content := make(map[string]any, len(pe.fields))

	// Save the current field value
	if len(pe.fields) > 0 {
		pe.fieldValues[pe.fields[pe.fieldIndex]] = pe.inputValue
	}

	for _, f := range pe.fields {
		content[f] = pe.fieldValues[f]
	}

	// If no fields were extracted (simple confirm), return empty content
	if len(pe.fields) == 0 {
		content = nil
	}

	pe.request.Response <- elicitationResponsePayload{
		Result: copilot.ElicitationResult{
			Action:  "accept",
			Content: content,
		},
	}

	m.pendingElicitation = nil
	m.updateDimensions()
	m.updateViewportContent()

	return m, m.waitForEvent()
}

// declineElicitation declines the elicitation request.
func (m *Model) declineElicitation() (tea.Model, tea.Cmd) {
	m.pendingElicitation.request.Response <- elicitationResponsePayload{
		Result: copilot.ElicitationResult{
			Action: "decline",
		},
	}

	m.pendingElicitation = nil
	m.updateDimensions()
	m.updateViewportContent()

	return m, m.waitForEvent()
}

// cancelElicitation cancels the elicitation request (used on quit).
func (m *Model) cancelElicitation() {
	m.pendingElicitation.request.Response <- elicitationResponsePayload{
		Result: copilot.ElicitationResult{
			Action: "cancel",
		},
	}

	m.pendingElicitation = nil
}

// renderElicitationModal renders the elicitation prompt as an inline modal section.
func (m *Model) renderElicitationModal() string {
	if m.pendingElicitation == nil {
		return ""
	}

	pe := m.pendingElicitation
	req := pe.request
	modalWidth := max(m.width-modalPadding, 1)
	mStyles := newModalContentStyles(modalWidth)

	var content strings.Builder

	contentLines := 0

	// Title
	title := "📋 Input Requested"
	if req.Source != "" {
		title += " (" + req.Source + ")"
	}

	content.WriteString(mStyles.clipStyle.Render(mStyles.warningStyle.Render(title)) + "\n\n")

	contentLines += 2

	// Message
	if req.Message != "" {
		content.WriteString(mStyles.clipStyle.Render(req.Message) + "\n\n")

		contentLines += 2
	}

	// URL mode: just show the URL
	if req.Mode == "url" && req.URL != "" {
		content.WriteString(mStyles.clipStyle.Render("Open: "+req.URL) + "\n\n")

		contentLines += 2
	}

	// Form fields
	if len(pe.fields) > 0 {
		for i, f := range pe.fields {
			prefix := "  "
			if i == pe.fieldIndex {
				prefix = "▸ "
			}

			val := pe.fieldValues[f]
			if i == pe.fieldIndex {
				val = pe.inputValue
			}

			content.WriteString(mStyles.clipStyle.Render(fmt.Sprintf("%s%s: %s", prefix, f, val)) + "\n")

			contentLines++
		}

		content.WriteString("\n")

		contentLines++
	}

	// Instructions
	instructions := "[Enter] Accept • [Esc] Decline"
	if len(pe.fields) > 1 {
		instructions = "[Tab/Shift+Tab] Navigate • " + instructions
	}

	content.WriteString(mStyles.clipStyle.Render(instructions) + "\n")

	contentLines++

	modalStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.ANSIColor(ansiCyan)).
		PaddingLeft(1).
		PaddingRight(1).
		Width(modalWidth).
		Height(contentLines)

	return modalStyle.Render(strings.TrimRight(content.String(), "\n"))
}

// CreateTUIElicitationHandler creates an elicitation handler that integrates with the TUI.
// It sends elicitation requests to the event channel and waits for the TUI to respond.
func CreateTUIElicitationHandler(eventChan chan<- tea.Msg) copilot.ElicitationHandler {
	return func(ctx copilot.ElicitationContext) (copilot.ElicitationResult, error) {
		responseChan := make(chan elicitationResponsePayload, 1)

		eventChan <- elicitationRequestMsg{
			Message:  ctx.Message,
			Source:   ctx.ElicitationSource,
			Mode:     ctx.Mode,
			URL:      ctx.URL,
			Schema:   ctx.RequestedSchema,
			Response: responseChan,
		}

		response := <-responseChan

		return response.Result, nil
	}
}

// extractFieldNames extracts field names from a JSON Schema "properties" map.
// Returns the field names in sorted order, or a single "value" field for simple schemas.
func extractFieldNames(schema map[string]any) []string {
	if schema == nil {
		return nil
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		return nil
	}

	fields := make([]string, 0, len(props))
	for k := range props {
		fields = append(fields, k)
	}

	return fields
}
