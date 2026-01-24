package chat

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	copilot "github.com/github/copilot-sdk/go"

	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
)

// CreatePermissionHandler creates a permission handler that prompts the user
// for confirmation on write operations.
func CreatePermissionHandler(writer io.Writer) copilot.PermissionHandler {
	return func(request copilot.PermissionRequest, _ copilot.PermissionInvocation) (copilot.PermissionRequestResult, error) {
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
func promptForPermission(writer io.Writer, request copilot.PermissionRequest) (copilot.PermissionRequestResult, error) {
	// Display permission request details
	fmt.Fprintln(writer, "")

	// Build a descriptive message
	desc := getPermissionDescription(request)
	if desc != "" {
		// Show the command/action being requested
		fmt.Fprintf(writer, "┌─ Permission Required (%s)\n", request.Kind)
		for _, line := range strings.Split(desc, "\n") {
			if line != "" {
				fmt.Fprintf(writer, "│  %s\n", line)
			}
		}
		fmt.Fprint(writer, "└─")
	} else {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("Permission requested: %s", request.Kind),
			Writer:  writer,
		})
	}

	// Prompt for confirmation
	fmt.Fprint(writer, "Allow this operation? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
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
	if args, ok := request.Extra["args"].([]interface{}); ok && len(args) > 0 {
		argStrs := make([]string, len(args))
		for i, arg := range args {
			argStrs[i] = fmt.Sprintf("%v", arg)
		}
		parts = append(parts, fmt.Sprintf("Args: %s", strings.Join(argStrs, " ")))
	}

	// File path for write operations
	if path, ok := request.Extra["path"].(string); ok && path != "" {
		parts = append(parts, fmt.Sprintf("Path: %s", path))
	}

	// Content preview for writes (truncated)
	if content, ok := request.Extra["content"].(string); ok && content != "" {
		preview := content
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		parts = append(parts, fmt.Sprintf("Content:\n%s", preview))
	}

	return strings.Join(parts, "\n")
}
