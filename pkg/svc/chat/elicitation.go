package chat

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	copilot "github.com/github/copilot-sdk/go"
)

// CreateElicitationHandler creates a non-TUI elicitation handler that prompts via
// stdin/stdout. It shows the elicitation message and asks the user to accept or decline.
func CreateElicitationHandler(writer io.Writer) copilot.ElicitationHandler {
	return func(ctx copilot.ElicitationContext) (copilot.ElicitationResult, error) {
		_, _ = fmt.Fprintln(writer, "")
		_, _ = fmt.Fprintln(writer, "┌─ Input Requested")

		if ctx.ElicitationSource != "" {
			_, _ = fmt.Fprintf(writer, "│  Source: %s\n", ctx.ElicitationSource)
		}

		if ctx.Message != "" {
			_, _ = fmt.Fprintf(writer, "│  %s\n", ctx.Message)
		}

		if ctx.Mode == "url" && ctx.URL != "" {
			_, _ = fmt.Fprintf(writer, "│  Open: %s\n", ctx.URL)
		}

		_, _ = fmt.Fprint(writer, "└─\n")

		// For form-mode with schema fields, collect field values
		if ctx.Mode == "form" && ctx.RequestedSchema != nil {
			return promptElicitationFields(writer, ctx.RequestedSchema)
		}

		// Simple accept/decline for URL mode or schema-less requests
		return promptElicitationAccept(writer)
	}
}

// promptElicitationAccept asks the user to accept or decline.
func promptElicitationAccept(writer io.Writer) (copilot.ElicitationResult, error) {
	_, _ = fmt.Fprint(writer, "Accept? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)

	line, readErr := reader.ReadString('\n')
	if readErr != nil {
		// stdin EOF/pipe-close → cancel the elicitation gracefully
		return copilot.ElicitationResult{Action: "cancel"}, nil //nolint:nilerr // intentional: EOF cancels
	}

	input := strings.TrimSpace(strings.ToLower(line))

	if input == "y" || input == "yes" {
		return copilot.ElicitationResult{Action: "accept"}, nil
	}

	return copilot.ElicitationResult{Action: "decline"}, nil
}

// promptElicitationFields prompts for each field in the schema.
func promptElicitationFields(writer io.Writer, schema map[string]any) (copilot.ElicitationResult, error) {
	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		return promptElicitationAccept(writer)
	}

	// Sort field names for deterministic prompt order
	fieldNames := make([]string, 0, len(props))
	for field := range props {
		fieldNames = append(fieldNames, field)
	}

	sort.Strings(fieldNames)

	reader := bufio.NewReader(os.Stdin)
	content := make(map[string]any, len(props))

	for _, field := range fieldNames {
		_, _ = fmt.Fprintf(writer, "%s: ", field)

		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			// stdin EOF/pipe-close → cancel the elicitation gracefully
			return copilot.ElicitationResult{Action: "cancel"}, nil //nolint:nilerr // intentional: EOF cancels
		}

		content[field] = strings.TrimSpace(line)
	}

	return copilot.ElicitationResult{
		Action:  "accept",
		Content: content,
	}, nil
}
