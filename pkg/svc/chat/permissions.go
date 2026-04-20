package chat

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	chatui "github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	copilot "github.com/github/copilot-sdk/go"
)

// CreatePermissionHandler creates a permission handler that manages user consent
// for tool operations. Read operations with kind "read" or "url" are auto-approved,
// while write operations require explicit user confirmation via an interactive prompt.
//
// The handler displays operation details in a formatted box showing:
//   - Tool name being executed
//   - Shell command (for command execution)
//   - File path (from FileName or Path fields)
//   - Diff preview for write operations (truncated to 200 chars)
func CreatePermissionHandler(writer io.Writer) copilot.PermissionHandlerFunc {
	return func(
		request copilot.PermissionRequest, _ copilot.PermissionInvocation,
	) (copilot.PermissionRequestResult, error) {
		// Auto-approve read operations
		if chatui.IsReadOperation(request.Kind) {
			return copilot.PermissionRequestResult{
				Kind: copilot.PermissionRequestResultKindApproved,
			}, nil
		}

		// Prompt for write operations
		return promptForPermission(writer, request)
	}
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
			Content: "Permission requested: " + string(request.Kind),
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
			Kind: copilot.PermissionRequestResultKindDeniedCouldNotRequestFromUser,
		}, nil
	}

	if strings.TrimSpace(line) == "" {
		return copilot.PermissionRequestResult{
			Kind: copilot.PermissionRequestResultKindDeniedInteractivelyByUser,
		}, nil
	}

	input := strings.TrimSpace(strings.ToLower(line))

	if input == "y" || input == "yes" {
		notify.WriteMessage(notify.Message{
			Type:    notify.SuccessType,
			Content: "Permission granted",
			Writer:  writer,
		})

		return copilot.PermissionRequestResult{
			Kind: copilot.PermissionRequestResultKindApproved,
		}, nil
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: "Permission denied",
		Writer:  writer,
	})

	return copilot.PermissionRequestResult{
		Kind: copilot.PermissionRequestResultKindDeniedInteractivelyByUser,
	}, nil
}

// getPermissionDescription extracts a human-readable description from the permission request.
func getPermissionDescription(request copilot.PermissionRequest) string {
	var parts []string

	parts = appendToolName(parts, request.ToolName)
	parts = appendCommand(parts, request.FullCommandText)
	parts = appendPath(parts, request.Path, request.FileName)
	parts = appendDiffPreview(parts, request.Diff)

	return strings.Join(parts, "\n")
}

// appendToolName appends the tool name to parts if present.
func appendToolName(parts []string, toolName *string) []string {
	if toolName != nil && *toolName != "" {
		return append(parts, "Tool: "+*toolName)
	}

	return parts
}

// appendCommand appends the shell command to parts if present.
func appendCommand(parts []string, fullCommandText *string) []string {
	if fullCommandText != nil && *fullCommandText != "" {
		return append(parts, "$ "+*fullCommandText)
	}

	return parts
}

// appendPath appends the file path to parts if present.
func appendPath(parts []string, path *string, fileName *string) []string {
	if fileName != nil && *fileName != "" {
		return append(parts, "Path: "+*fileName)
	}

	if path != nil && *path != "" {
		return append(parts, "Path: "+*path)
	}

	return parts
}

// appendDiffPreview appends a truncated diff preview to parts if present.
func appendDiffPreview(parts []string, diff *string) []string {
	const maxContentPreview = 200

	if diff == nil || *diff == "" {
		return parts
	}

	preview := *diff
	if len(preview) > maxContentPreview {
		preview = preview[:maxContentPreview] + "..."
	}

	return append(parts, "Diff:\n"+preview)
}
