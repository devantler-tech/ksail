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

	return readPermissionResponse(writer)
}

// readPermissionResponse reads and processes the user's permission response from stdin.
func readPermissionResponse(
	writer io.Writer,
) (copilot.PermissionRequestResult, error) {
	_, _ = fmt.Fprint(writer, "Allow this operation? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)

	line, readErr := reader.ReadString('\n')

	// I/O error (typically EOF) is expected in non-interactive contexts.
	// Treat it as a denial rather than propagating the error.
	//nolint:nilerr // I/O errors (EOF) treated as denial in non-interactive contexts
	if readErr != nil {
		return copilot.PermissionRequestResult{
			Kind: "denied-no-approval-rule-and-could-not-request-from-user",
		}, nil
	}

	if strings.TrimSpace(line) == "" {
		return copilot.PermissionRequestResult{
			Kind: "denied-no-approval-rule-and-could-not-request-from-user",
		}, nil
	}

	input := strings.TrimSpace(strings.ToLower(line))

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

	parts = appendToolName(parts, request.Extra)
	parts = appendCommand(parts, request.Extra)
	parts = appendArgs(parts, request.Extra)
	parts = appendPath(parts, request.Extra)
	parts = appendContentPreview(parts, request.Extra)

	return strings.Join(parts, "\n")
}

// appendToolName appends the tool name to parts if present.
func appendToolName(parts []string, extra map[string]any) []string {
	if tool, ok := extra["toolName"].(string); ok && tool != "" {
		return append(parts, "Tool: "+tool)
	}

	return parts
}

// appendCommand appends the shell command to parts if present.
func appendCommand(parts []string, extra map[string]any) []string {
	if cmd, ok := extra["command"].(string); ok && cmd != "" {
		return append(parts, "$ "+cmd)
	}

	return parts
}

// appendArgs appends the arguments to parts if present.
func appendArgs(parts []string, extra map[string]any) []string {
	args, ok := extra["args"].([]any)
	if !ok || len(args) == 0 {
		return parts
	}

	argStrs := make([]string, len(args))
	for i, arg := range args {
		argStrs[i] = fmt.Sprintf("%v", arg)
	}

	return append(parts, "Args: "+strings.Join(argStrs, " "))
}

// appendPath appends the file path to parts if present.
func appendPath(parts []string, extra map[string]any) []string {
	if path, ok := extra["path"].(string); ok && path != "" {
		return append(parts, "Path: "+path)
	}

	return parts
}

// appendContentPreview appends a truncated content preview to parts if present.
func appendContentPreview(parts []string, extra map[string]any) []string {
	const maxContentPreview = 200

	content, ok := extra["content"].(string)
	if !ok || content == "" {
		return parts
	}

	preview := content
	if len(preview) > maxContentPreview {
		preview = preview[:maxContentPreview] + "..."
	}

	return append(parts, "Content:\n"+preview)
}
