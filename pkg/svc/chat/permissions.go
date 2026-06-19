package chat

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/notify"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
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
	) (rpc.PermissionDecision, error) {
		// Auto-approve read operations
		if IsReadOperation(request.Kind()) {
			return &rpc.PermissionDecisionApproveOnce{}, nil
		}

		// Prompt for write operations
		return promptForPermission(writer, request)
	}
}

// promptForPermission prompts the user for permission and returns the result.
func promptForPermission(
	writer io.Writer,
	request copilot.PermissionRequest,
) (rpc.PermissionDecision, error) {
	// Display permission request details
	_, _ = fmt.Fprintln(writer, "")

	// Build a descriptive message
	desc := getPermissionDescription(request)
	if desc != "" {
		// Show the command/action being requested
		_, _ = fmt.Fprintf(writer, "┌─ Permission Required (%s)\n", request.Kind())

		for line := range strings.SplitSeq(desc, "\n") {
			if line != "" {
				_, _ = fmt.Fprintf(writer, "│  %s\n", line)
			}
		}

		_, _ = fmt.Fprint(writer, "└─")
	} else {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "Permission requested: " + string(request.Kind()),
			Writer:  writer,
		})
	}

	return readPermissionResponse(writer)
}

// readPermissionResponse reads and processes the user's permission response from stdin.
func readPermissionResponse(
	writer io.Writer,
) (rpc.PermissionDecision, error) {
	_, _ = fmt.Fprint(writer, "Allow this operation? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)

	line, readErr := reader.ReadString('\n')

	// I/O error (typically EOF) is expected in non-interactive contexts.
	// Treat it as "user not available" rather than propagating the error.
	//nolint:nilerr // I/O errors (EOF) treated as denial in non-interactive contexts
	if readErr != nil {
		return &rpc.PermissionDecisionUserNotAvailable{}, nil
	}

	if strings.TrimSpace(line) == "" {
		return &rpc.PermissionDecisionReject{}, nil
	}

	input := strings.TrimSpace(strings.ToLower(line))

	if input == "y" || input == "yes" {
		notify.WriteMessage(notify.Message{
			Type:    notify.SuccessType,
			Content: "Permission granted",
			Writer:  writer,
		})

		return &rpc.PermissionDecisionApproveOnce{}, nil
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: "Permission denied",
		Writer:  writer,
	})

	return &rpc.PermissionDecisionReject{}, nil
}

// getPermissionDescription extracts a human-readable description from the permission request.
// In v1.0.0 the SDK delivers permission requests as discriminated pointer types, so the
// relevant fields are read via a type switch over the concrete request variants.
func getPermissionDescription(request copilot.PermissionRequest) string {
	switch req := request.(type) {
	case *copilot.PermissionRequestCustomTool:
		return labeled("Tool: ", req.ToolName)
	case *copilot.PermissionRequestMCP:
		return labeled("Tool: ", req.ToolName)
	case *copilot.PermissionRequestShell:
		return labeled("$ ", req.FullCommandText)
	case *copilot.PermissionRequestRead:
		return labeled("Path: ", req.Path)
	case *copilot.PermissionRequestURL:
		return labeled("URL: ", req.URL)
	case *copilot.PermissionRequestWrite:
		return writePermissionDescription(req)
	case *copilot.PermissionRequestExtensionManagement:
		return extensionManagementDescription(req)
	case *copilot.PermissionRequestExtensionPermissionAccess:
		return labeled("Extension: ", req.ExtensionName)
	default:
		return ""
	}
}

// extensionManagementDescription renders the operation and (when present) the
// extension name for an extension-management permission request.
func extensionManagementDescription(req *copilot.PermissionRequestExtensionManagement) string {
	var parts []string

	if op := labeled("Operation: ", req.Operation); op != "" {
		parts = append(parts, op)
	}

	if req.ExtensionName != nil {
		if name := labeled("Extension: ", *req.ExtensionName); name != "" {
			parts = append(parts, name)
		}
	}

	return strings.Join(parts, "\n")
}

// labeled returns "label+value" when value is non-empty, otherwise an empty string.
func labeled(label, value string) string {
	if value == "" {
		return ""
	}

	return label + value
}

// writePermissionDescription renders the target path and a truncated diff preview
// for a file-write permission request.
func writePermissionDescription(req *copilot.PermissionRequestWrite) string {
	const maxDiffPreview = 200

	var parts []string

	if req.FileName != "" {
		parts = append(parts, "Path: "+req.FileName)
	}

	if req.Diff != "" {
		preview := req.Diff
		if len(preview) > maxDiffPreview {
			preview = preview[:maxDiffPreview] + "..."
		}

		parts = append(parts, "Diff:\n"+preview)
	}

	return strings.Join(parts, "\n")
}
