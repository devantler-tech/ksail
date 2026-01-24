package chat

import (
	"fmt"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	copilot "github.com/github/copilot-sdk/go"
)

// PermissionBridge coordinates permission requests between the Copilot SDK and the TUI.
// It holds a reference to the tea.Program for thread-safe message delivery.
type PermissionBridge struct {
	program *tea.Program
	mu      sync.RWMutex
}

// NewPermissionBridge creates a new permission bridge.
// Call SetProgram() before using the handler.
func NewPermissionBridge() *PermissionBridge {
	return &PermissionBridge{}
}

// SetProgram sets the tea.Program reference. Must be called before any permission requests.
func (b *PermissionBridge) SetProgram(p *tea.Program) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.program = p
}

// Handler returns a permission handler function for the Copilot SDK.
func (b *PermissionBridge) Handler() copilot.PermissionHandler {
	return func(
		request copilot.PermissionRequest,
		_ copilot.PermissionInvocation,
	) (copilot.PermissionRequestResult, error) {
		// Auto-approve read operations - they don't modify anything
		if request.Kind == "read" || request.Kind == "url" {
			return copilot.PermissionRequestResult{Kind: "approved"}, nil
		}

		b.mu.RLock()
		program := b.program
		b.mu.RUnlock()

		if program == nil {
			// Program not ready - deny for safety
			return copilot.PermissionRequestResult{
				Kind: "denied-interactively-by-user",
			}, nil
		}

		// Create a response channel for this specific request
		respChan := make(chan copilot.PermissionRequestResult, 1)

		// Send permission request to TUI via tea.Program.Send() (thread-safe)
		program.Send(permissionRequestMsg{
			request:  request,
			respChan: respChan,
		})

		// Block waiting for user response from TUI
		result := <-respChan
		return result, nil
	}
}

// permissionRequestMsg signals that a permission request needs user interaction.
// The respChan is used by the TUI to send back the user's decision.
type permissionRequestMsg struct {
	request  copilot.PermissionRequest
	respChan chan<- copilot.PermissionRequestResult
}

// formatPermissionDesc formats the permission request details for display.
func formatPermissionDesc(request copilot.PermissionRequest) string {
	if request.Extra == nil {
		return request.Kind
	}

	var parts []string

	// Tool name
	if tool, ok := request.Extra["toolName"].(string); ok && tool != "" {
		parts = append(parts, fmt.Sprintf("Tool: %s", tool))
	}

	// Shell command - most important for shell operations
	if cmd, ok := request.Extra["command"].(string); ok && cmd != "" {
		// Truncate very long commands
		if len(cmd) > 200 {
			cmd = cmd[:200] + "..."
		}
		parts = append(parts, fmt.Sprintf("$ %s", cmd))
	}

	// File path for write operations
	if path, ok := request.Extra["path"].(string); ok && path != "" {
		parts = append(parts, fmt.Sprintf("Path: %s", path))
	}

	// Content preview for writes (truncated)
	if content, ok := request.Extra["content"].(string); ok && content != "" {
		preview := content
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		preview = strings.ReplaceAll(preview, "\n", "↵")
		parts = append(parts, fmt.Sprintf("Content: %s", preview))
	}

	if len(parts) == 0 {
		return request.Kind
	}
	return strings.Join(parts, "\n")
}
