package chat

import (
	"fmt"
	"sort"
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

	switch msg.String() {
	case keyEnter:
		return m.acceptElicitation()
	case keyEscape:
		return m.declineElicitation()
	case keyCtrlC:
		m.cancelElicitation()
		m.cleanup()
		m.quitting = true

		return m, tea.Quit
	case keyTab:
		return m.navigateElicitationField(1)
	case "shift+tab":
		return m.navigateElicitationField(-1)
	case "backspace":
		m.handleElicitationBackspace()

		return m, nil
	default:
		m.handleElicitationRune(msg)

		return m, nil
	}
}

// handleElicitationBackspace removes the last character from the elicitation input.
func (m *Model) handleElicitationBackspace() {
	pending := m.pendingElicitation
	if len(pending.inputValue) > 0 {
		pending.inputValue = pending.inputValue[:len(pending.inputValue)-1]
	}
}

// handleElicitationRune appends typed runes to the elicitation input.
func (m *Model) handleElicitationRune(msg tea.KeyMsg) {
	if len(msg.Runes) > 0 {
		m.pendingElicitation.inputValue += string(msg.Runes)
	}
}

// navigateElicitationField moves the field cursor by the given delta, saving the current value.
func (m *Model) navigateElicitationField(delta int) (tea.Model, tea.Cmd) {
	pending := m.pendingElicitation
	if len(pending.fields) <= 1 {
		return m, nil
	}

	pending.fieldValues[pending.fields[pending.fieldIndex]] = pending.inputValue
	pending.fieldIndex = (pending.fieldIndex + len(pending.fields) + delta) % len(pending.fields)
	pending.inputValue = pending.fieldValues[pending.fields[pending.fieldIndex]]

	return m, nil
}

// acceptElicitation submits the elicitation with the current input values.
func (m *Model) acceptElicitation() (tea.Model, tea.Cmd) {
	pending := m.pendingElicitation
	content := make(map[string]any, len(pending.fields))

	// Save the current field value
	if len(pending.fields) > 0 {
		pending.fieldValues[pending.fields[pending.fieldIndex]] = pending.inputValue
	}

	for _, field := range pending.fields {
		content[field] = pending.fieldValues[field]
	}

	// If no fields were extracted (simple confirm), return empty content
	if len(pending.fields) == 0 {
		content = nil
	}

	pending.request.Response <- elicitationResponsePayload{
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

	pending := m.pendingElicitation
	req := pending.request
	modalWidth := max(m.width-modalPadding, 1)
	mStyles := newModalContentStyles(modalWidth)

	var content strings.Builder

	contentLines := m.renderElicitationHeader(&content, req, mStyles)
	contentLines += m.renderElicitationFields(&content, pending, mStyles)

	// Instructions
	instructions := "[Enter] Accept • [Esc] Decline"
	if len(pending.fields) > 1 {
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

// renderElicitationHeader writes the title, message, and URL sections. Returns the line count.
func (m *Model) renderElicitationHeader(
	content *strings.Builder, req *elicitationRequestMsg, mStyles modalContentStyles,
) int {
	lines := 0

	title := "📋 Input Requested"
	if req.Source != "" {
		title += " (" + req.Source + ")"
	}

	content.WriteString(mStyles.clipStyle.Render(mStyles.warningStyle.Render(title)) + "\n\n")

	lines += 2 //nolint:mnd // title + blank line

	if req.Message != "" {
		content.WriteString(mStyles.clipStyle.Render(req.Message) + "\n\n")

		lines += 2 //nolint:mnd // message + blank line
	}

	if req.Mode == "url" && req.URL != "" {
		content.WriteString(mStyles.clipStyle.Render("Open: "+req.URL) + "\n\n")

		lines += 2 //nolint:mnd // URL + blank line
	}

	return lines
}

// renderElicitationFields writes the form field section. Returns the line count.
func (m *Model) renderElicitationFields(
	content *strings.Builder, pending *pendingElicitation, mStyles modalContentStyles,
) int {
	if len(pending.fields) == 0 {
		return 0
	}

	lines := 0

	for fieldIdx, field := range pending.fields {
		prefix := "  "
		if fieldIdx == pending.fieldIndex {
			prefix = "▸ "
		}

		val := pending.fieldValues[field]
		if fieldIdx == pending.fieldIndex {
			val = pending.inputValue
		}

		content.WriteString(mStyles.clipStyle.Render(fmt.Sprintf("%s%s: %s", prefix, field, val)) + "\n")

		lines++
	}

	content.WriteString("\n")

	lines++

	return lines
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
// Returns the field names in sorted order, or nil for empty/missing schemas.
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

	sort.Strings(fields)

	return fields
}
