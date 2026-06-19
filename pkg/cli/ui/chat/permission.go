package chat

import (
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	chatsvc "github.com/devantler-tech/ksail/v7/pkg/svc/chat"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
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

	// While collecting an optional denial reason, route keys to the deny-input handler.
	if m.permissionDenyInput {
		return m.handlePermissionDenyKey(msg)
	}

	switch msg.String() {
	case "y", "Y":
		return m.allowPermission()
	case "s", "S":
		if m.pendingPermission.canOfferSessionApproval {
			return m.allowPermissionForSession()
		}

		return m.allowPermission()
	case "a", "A":
		return m.allowAlwaysPermission()
	case "n", "N", keyEscape:
		return m.beginDenyPermission()
	case keyCtrlC:
		m.sendPermissionResponse(permissionResponse{approved: false})

		m.pendingPermission = nil

		return m.handleQuit()
	}

	return m, nil
}

// handlePermissionDenyKey handles keyboard input while collecting an optional denial reason.
// Enter submits the reason (empty allowed), Esc cancels back to the allow/deny prompt, and
// Ctrl+C aborts the session.
func (m *Model) handlePermissionDenyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEnter:
		return m.submitDenyPermission()
	case keyEscape:
		// Cancel reason entry and return to the allow/deny prompt.
		m.permissionDenyInput = false
		m.permissionDenyValue = ""
		m.updateDimensions()
		m.updateViewportContent()

		return m, nil
	case keyCtrlC:
		m.sendPermissionResponse(permissionResponse{approved: false})
		m.resetPermissionState()

		return m.handleQuit()
	case keyBackspace:
		if m.permissionDenyValue != "" {
			m.permissionDenyValue = m.permissionDenyValue[:len(m.permissionDenyValue)-1]
		}

		return m, nil
	default:
		if len(msg.Runes) > 0 {
			m.permissionDenyValue += string(msg.Runes)
		}

		return m, nil
	}
}

// sendPermissionResponse forwards the decision to the waiting SDK handler, if any.
func (m *Model) sendPermissionResponse(resp permissionResponse) {
	if m.pendingPermission != nil {
		m.pendingPermission.response <- resp
	}
}

// resetPermissionState clears all permission-prompt state without recomputing layout.
func (m *Model) resetPermissionState() {
	m.pendingPermission = nil
	m.permissionDenyInput = false
	m.permissionDenyValue = ""
}

// clearPendingPermission resets all permission-prompt state and recomputes layout.
func (m *Model) clearPendingPermission() {
	m.resetPermissionState()
	m.updateDimensions()
	m.updateViewportContent()
}

// allowPermission approves the pending permission request.
func (m *Model) allowPermission() (tea.Model, tea.Cmd) {
	m.sendPermissionResponse(permissionResponse{approved: true})

	m.clearPendingPermission()

	return m, m.waitForEvent()
}

// allowPermissionForSession approves the pending permission request for this session
// when session-scoped approvals are supported by the request kind.
func (m *Model) allowPermissionForSession() (tea.Model, tea.Cmd) {
	m.sendPermissionResponse(permissionResponse{approved: true, approveForSession: true})

	m.clearPendingPermission()

	return m, m.waitForEvent()
}

// allowAlwaysPermission approves the pending permission request and switches to Autopilot mode
// so all future permission requests are auto-approved for the rest of the session.
func (m *Model) allowAlwaysPermission() (tea.Model, tea.Cmd) {
	err := m.applyMode(AutopilotMode)
	if err != nil {
		m.err = err
	} else {
		m.chatMode = AutopilotMode
	}

	return m.allowPermission()
}

// beginDenyPermission switches the prompt into reason-entry mode, where the user can type an
// optional free-text denial reason before confirming with Enter.
func (m *Model) beginDenyPermission() (tea.Model, tea.Cmd) {
	m.permissionDenyInput = true
	m.permissionDenyValue = ""
	m.updateDimensions()
	m.updateViewportContent()

	return m, nil
}

// submitDenyPermission denies the pending permission request, forwarding any typed reason as
// feedback. An empty reason denies without feedback.
func (m *Model) submitDenyPermission() (tea.Model, tea.Cmd) {
	feedback := strings.TrimSpace(m.permissionDenyValue)

	m.sendPermissionResponse(permissionResponse{approved: false, feedback: feedback})

	m.clearPendingPermission()

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

	contentLines += m.renderPermissionPromptSection(&content, mStyles)

	modalStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.ANSIColor(ansiYellow)).
		PaddingLeft(1).
		PaddingRight(1).
		Width(modalWidth).
		Height(contentLines)

	return modalStyle.Render(strings.TrimRight(content.String(), "\n"))
}

// Content-line counts contributed by the permission modal's bottom prompt section, used to
// size the modal height.
const (
	denyInputPromptLines = 3 // blank line + reason-input line (+ spacing)
	allowPromptLines     = 4 // blank line + "Allow this operation?" + key-hint line (+ spacing)
)

// renderPermissionPromptSection writes the bottom section of the permission modal — either the
// optional-reason input (when denying) or the allow/deny prompt with key hints — and returns
// the number of content lines it added.
func (m *Model) renderPermissionPromptSection(
	content *strings.Builder, mStyles modalContentStyles,
) int {
	if m.permissionDenyInput {
		reasonLine := "Reason for denial (optional): " + m.permissionDenyValue
		content.WriteString("\n" + mStyles.clipStyle.Render(reasonLine) + "\n")

		return denyInputPromptLines
	}

	content.WriteString("\n" + mStyles.clipStyle.Render("Allow this operation?") + "\n")
	content.WriteString(
		mStyles.clipStyle.Render(permissionPromptHint(m.pendingPermission)) + "\n",
	)

	return allowPromptLines
}

// permissionDeduplicator tracks approved ToolCallIDs to prevent duplicate permission prompts.
// The CLI server may deliver the same permission request via both the V3 broadcast
// (session.event) and V2 RPC (permission.request) protocols.
type permissionDeduplicator struct {
	mu      sync.Mutex
	allowed map[string]struct{}
}

func newPermissionDeduplicator() *permissionDeduplicator {
	return &permissionDeduplicator{allowed: make(map[string]struct{})}
}

func (d *permissionDeduplicator) wasApproved(toolCallID string) bool {
	if toolCallID == "" {
		return false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	_, ok := d.allowed[toolCallID]

	return ok
}

func (d *permissionDeduplicator) markApproved(toolCallID string) {
	if toolCallID == "" {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.allowed[toolCallID] = struct{}{}
}

// CreateTUIPermissionHandler creates a permission handler that integrates with the TUI.
// It sends permission requests to the provided event channel and waits for a response.
// This allows the TUI to display permission prompts and collect user input.
// Read and URL operations are auto-approved to avoid excessive prompting.
// When chatModeRef is provided and Autopilot mode is enabled, permissions are auto-approved.
func CreateTUIPermissionHandler(
	eventChan chan<- tea.Msg,
	chatModeRef *ChatModeRef,
) copilot.PermissionHandlerFunc {
	dedup := newPermissionDeduplicator()

	return func(
		request copilot.PermissionRequest,
		_ copilot.PermissionInvocation,
	) (rpc.PermissionDecision, error) {
		// In Autopilot mode, auto-approve all SDK permission requests
		if chatModeRef != nil && chatModeRef.Mode() == AutopilotMode {
			return &rpc.PermissionDecisionApproveOnce{}, nil
		}

		// Auto-approve read operations to avoid excessive prompting.
		if chatsvc.IsReadOperation(request.Kind()) {
			return &rpc.PermissionDecisionApproveOnce{}, nil
		}

		toolCallID := permissionToolCallID(request)

		// Deduplicate: auto-approve if this ToolCallID was already approved.
		if dedup.wasApproved(toolCallID) {
			return &rpc.PermissionDecisionApproveOnce{}, nil
		}

		toolName, command := extractPermissionDetails(request)
		responseChan := make(chan permissionResponse, 1)

		eventChan <- permissionRequestMsg{
			toolCallID:              toolCallID,
			toolName:                toolName,
			command:                 command,
			arguments:               permissionArguments(request),
			response:                responseChan,
			canOfferSessionApproval: canOfferSessionApproval(request),
		}

		resp := <-responseChan
		if resp.approved {
			dedup.markApproved(toolCallID)

			if resp.approveForSession {
				if approval := sessionApproval(request); approval != nil {
					return &rpc.PermissionDecisionApproveForSession{Approval: approval}, nil
				}
			}

			return &rpc.PermissionDecisionApproveOnce{}, nil
		}

		return rejectDecision(resp.feedback), nil
	}
}

// rejectDecision builds a rejection decision, attaching the optional feedback reason when
// non-empty (nil otherwise).
func rejectDecision(feedback string) rpc.PermissionDecision {
	if feedback == "" {
		return &rpc.PermissionDecisionReject{}
	}

	return &rpc.PermissionDecisionReject{Feedback: &feedback}
}

// canOfferSessionApproval reports whether KSail can build a correct session-scoped
// Approval for the given request. It is intentionally conservative: only Shell and
// Write requests are supported (both expose CanOfferSessionApproval and have a
// well-defined Approval variant), and only when the SDK itself permits it.
func canOfferSessionApproval(request copilot.PermissionRequest) bool {
	switch req := request.(type) {
	case *copilot.PermissionRequestShell:
		return req.CanOfferSessionApproval
	case *copilot.PermissionRequestWrite:
		return req.CanOfferSessionApproval
	default:
		return false
	}
}

// sessionApproval builds the session-scoped Approval describing what the SDK should
// remember for the rest of the session. Only the kinds reported by
// canOfferSessionApproval produce a meaningful Approval:
//   - Shell -> Commands keyed by the parsed command identifiers in the request.
//   - Write -> the empty Write approval (covers file writes for the session).
//
// For every other kind a nil Approval is returned; callers must only request a
// session approval for supported kinds.
func sessionApproval(
	request copilot.PermissionRequest,
) rpc.PermissionDecisionApproveForSessionApproval {
	switch req := request.(type) {
	case *copilot.PermissionRequestShell:
		identifiers := make([]string, 0, len(req.Commands))
		for _, cmd := range req.Commands {
			if cmd.Identifier != "" {
				identifiers = append(identifiers, cmd.Identifier)
			}
		}

		return rpc.PermissionDecisionApproveForSessionApprovalCommands{
			CommandIdentifiers: identifiers,
		}
	case *copilot.PermissionRequestWrite:
		return rpc.PermissionDecisionApproveForSessionApprovalWrite{}
	default:
		return nil
	}
}

// permissionPromptHint renders the key hint shown beneath the permission prompt.
// The "s" (session) option is only advertised when it is applicable.
func permissionPromptHint(req *permissionRequestMsg) string {
	if req != nil && req.canOfferSessionApproval {
		return "[y] allow once  [s] allow for session  [a] always (autopilot)  [n] deny"
	}

	return "[y] allow once  [a] always (autopilot)  [n] deny"
}

// extractPermissionDetails extracts human-readable tool name and command
// from an SDK permission request.
// The PermissionRequest has typed fields for each permission kind.
func extractPermissionDetails(request copilot.PermissionRequest) (string, string) {
	toolName := formatPermissionKind(request.Kind())

	if detail := permissionDetail(request); detail != "" {
		return toolName, detail
	}

	return toolName, string(request.Kind())
}

// permissionDetail returns the most relevant human-readable detail for a permission
// request. In v1.0.0 each permission kind is a distinct pointer type, so the relevant
// field is selected via a type switch over the concrete request variants.
func permissionDetail(request copilot.PermissionRequest) string {
	switch req := request.(type) {
	case *copilot.PermissionRequestShell:
		return req.FullCommandText
	case *copilot.PermissionRequestWrite:
		if req.FileName != "" {
			return req.FileName
		}

		return req.Diff
	case *copilot.PermissionRequestRead:
		return req.Path
	case *copilot.PermissionRequestURL:
		return req.URL
	case *copilot.PermissionRequestCustomTool:
		return req.ToolName
	case *copilot.PermissionRequestMCP:
		return req.ToolName
	}

	return extensionPermissionDetail(request)
}

// extensionPermissionDetail returns the human-readable detail for the
// extension-related permission request variants, or "" for any other type.
func extensionPermissionDetail(request copilot.PermissionRequest) string {
	switch req := request.(type) {
	case *copilot.PermissionRequestExtensionManagement:
		if name := derefString(req.ExtensionName); name != "" {
			return req.Operation + " " + name
		}

		return req.Operation
	case *copilot.PermissionRequestExtensionPermissionAccess:
		return req.ExtensionName
	}

	return ""
}

// permissionArguments returns a short, kind-specific line of extra context for a
// permission request, shown to the user under the "Arguments:" label in the modal.
// It returns "" when there's nothing useful to add for the request's kind.
func permissionArguments(request copilot.PermissionRequest) string {
	switch req := request.(type) {
	case *copilot.PermissionRequestShell:
		if warning := derefString(req.Warning); warning != "" {
			return "⚠ " + warning
		}
	case *copilot.PermissionRequestMCP:
		if req.ServerName == "" {
			return ""
		}

		detail := "Server: " + req.ServerName
		if req.ReadOnly {
			detail += " (read-only)"
		}

		return detail
	case *copilot.PermissionRequestWrite:
		if req.NewFileContents != nil {
			return "New file"
		}
	}

	return ""
}

// permissionToolCallID extracts the tool-call ID from a permission request, if present.
// Each concrete request variant carries its own optional ToolCallID field.
func permissionToolCallID(request copilot.PermissionRequest) string {
	switch req := request.(type) {
	case *copilot.PermissionRequestShell:
		return derefString(req.ToolCallID)
	case *copilot.PermissionRequestWrite:
		return derefString(req.ToolCallID)
	case *copilot.PermissionRequestRead:
		return derefString(req.ToolCallID)
	case *copilot.PermissionRequestURL:
		return derefString(req.ToolCallID)
	case *copilot.PermissionRequestCustomTool:
		return derefString(req.ToolCallID)
	case *copilot.PermissionRequestMCP:
		return derefString(req.ToolCallID)
	case *copilot.PermissionRequestMemory:
		return derefString(req.ToolCallID)
	case *copilot.PermissionRequestHook:
		return derefString(req.ToolCallID)
	}

	return extensionPermissionToolCallID(request)
}

// extensionPermissionToolCallID extracts the tool-call ID from the
// extension-related permission request variants, or "" for any other type.
func extensionPermissionToolCallID(request copilot.PermissionRequest) string {
	switch req := request.(type) {
	case *copilot.PermissionRequestExtensionManagement:
		return derefString(req.ToolCallID)
	case *copilot.PermissionRequestExtensionPermissionAccess:
		return derefString(req.ToolCallID)
	}

	return ""
}

// derefString returns the pointed-to string, or "" when the pointer is nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

// permissionKindLabels maps known permission kinds to human-readable tool names.
var permissionKindLabels = map[copilot.PermissionRequestKind]string{
	copilot.PermissionRequestKindShell:                     "Shell Command",
	copilot.PermissionRequestKindWrite:                     "File Write",
	copilot.PermissionRequestKindRead:                      "File Read",
	copilot.PermissionRequestKindURL:                       "URL",
	copilot.PermissionRequestKindMCP:                       "MCP Tool",
	copilot.PermissionRequestKindCustomTool:                "Custom Tool",
	copilot.PermissionRequestKindMemory:                    "Memory",
	copilot.PermissionRequestKindHook:                      "Hook",
	copilot.PermissionRequestKindExtensionManagement:       "Extension Management",
	copilot.PermissionRequestKindExtensionPermissionAccess: "Extension Access",
}

// formatPermissionKind converts a permission kind to a human-readable tool name.
func formatPermissionKind(kind copilot.PermissionRequestKind) string {
	if label, ok := permissionKindLabels[kind]; ok {
		return label
	}

	// Unknown kind: replace underscores with spaces and title-case it
	// (e.g. "file_edit" -> "File Edit"). English titlecase suits all SDK kinds.
	kindStr := string(kind)
	if kindStr == "" {
		return unknownOperation
	}

	formatted := strings.ReplaceAll(kindStr, "_", " ")
	caser := cases.Title(language.English)

	return caser.String(formatted)
}
