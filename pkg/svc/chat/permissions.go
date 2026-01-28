package chat

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	copilot "github.com/github/copilot-sdk/go"
)

// CreatePermissionHandler creates a permission handler that manages user consent
// for tool operations. Read operations with kind "read" or "url" are auto-approved,
// while write operations require explicit user confirmation via an interactive prompt.
//
// The handler displays operation details in a formatted box showing:
//   - Tool name being executed
//   - Shell command (for command execution)
//   - Arguments and file paths involved
//   - Content preview for write operations (truncated to 200 chars)
func CreatePermissionHandler(writer io.Writer) copilot.PermissionHandler {
	return func(
		request copilot.PermissionRequest, _ copilot.PermissionInvocation,
	) (copilot.PermissionRequestResult, error) {
		// Auto-approve read operations
		if isReadOperation(request.Kind) {
			return copilot.PermissionRequestResult{Kind: "approved"}, nil
		}

		// Prompt for write operations
		return promptForPermission(writer, request)
	}
}

// isReadOperation determines if a permission request is for a read-only operation.
func isReadOperation(kind string) bool {
	readKinds := map[string]bool{
		"read": true,
		"url":  true, // URL fetching is typically read-only
	}

	return readKinds[kind]
}

// promptForPermission prompts the user for permission and returns the result.
func promptForPermission(
	writer io.Writer,
	request copilot.PermissionRequest,
) (copilot.PermissionRequestResult, error) {
	// Display permission request details
	_, _ = fmt.Fprintln(writer, "")

	// Build a descriptive message
	desc := getPermissionDescription(request)
	if desc != "" {
		// Show the command/action being requested
		_, _ = fmt.Fprintf(writer, "┌─ Permission Required (%s)\n", request.Kind)

		for line := range strings.SplitSeq(desc, "\n") {
			if line != "" {
				_, _ = fmt.Fprintf(writer, "│  %s\n", line)
			}
		}

		_, _ = fmt.Fprint(writer, "└─")
	} else {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "Permission requested: " + request.Kind,
			Writer:  writer,
		})
	}

	// Prompt for confirmation
	_, _ = fmt.Fprint(writer, "Allow this operation? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)

	input, err := reader.ReadString('\n')
	if err != nil {
		// Return denied result. The error (typically EOF) is expected in non-interactive contexts.
		// We intentionally don't propagate it since the denied result handles this case.
		return copilot.PermissionRequestResult{
			Kind: "denied-no-approval-rule-and-could-not-request-from-user",
		}, nil
	}

	input = strings.TrimSpace(strings.ToLower(input))

	if input == "y" || input == "yes" {
		notify.WriteMessage(notify.Message{
			Type:    notify.SuccessType,
			Content: "Permission granted",
			Writer:  writer,
		})

		return copilot.PermissionRequestResult{Kind: "approved"}, nil
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: "Permission denied",
		Writer:  writer,
	})

	return copilot.PermissionRequestResult{Kind: "denied-interactively-by-user"}, nil
}

// getPermissionDescription extracts a human-readable description from the permission request.
func getPermissionDescription(request copilot.PermissionRequest) string {
	// Try to extract common fields from the Extra data
	if request.Extra == nil {
		return ""
	}

	var parts []string

	// Tool name if available - show first
	if tool, ok := request.Extra["toolName"].(string); ok && tool != "" {
		parts = append(parts, fmt.Sprintf("Tool: %s", tool))
	}

	// Shell command - most important for shell operations
	if cmd, ok := request.Extra["command"].(string); ok && cmd != "" {
		parts = append(parts, fmt.Sprintf("$ %s", cmd))
	}

	// Arguments if present
	if args, ok := request.Extra["args"].([]any); ok && len(args) > 0 {
		argStrs := make([]string, len(args))
		for i, arg := range args {
			argStrs[i] = fmt.Sprintf("%v", arg)
		}

		parts = append(parts, "Args: "+strings.Join(argStrs, " "))
	}

	// File path for write operations
	if path, ok := request.Extra["path"].(string); ok && path != "" {
		parts = append(parts, "Path: "+path)
	}

	// Content preview for writes (truncated)
	const maxContentPreview = 200

	if content, ok := request.Extra["content"].(string); ok && content != "" {
		preview := content
		if len(preview) > maxContentPreview {
			preview = preview[:maxContentPreview] + "..."
		}

		parts = append(parts, "Content:\n"+preview)
	}

	return strings.Join(parts, "\n")
}
