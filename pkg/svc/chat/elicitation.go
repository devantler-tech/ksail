package chat

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	copilot "github.com/github/copilot-sdk/go"
)

// CreateElicitationHandler creates a non-TUI elicitation handler that prompts via
// the provided reader/writer. It shows the elicitation message and asks the user
// to accept or decline.
func CreateElicitationHandler(reader io.Reader, writer io.Writer) copilot.ElicitationHandler {
	bufReader, ok := reader.(*bufio.Reader)
	if !ok {
		bufReader = bufio.NewReader(reader)
	}

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
			return promptElicitationFields(bufReader, writer, ctx.RequestedSchema)
		}

		// Simple accept/decline for URL mode or schema-less requests
		return promptElicitationAccept(bufReader, writer)
	}
}

// errElicitationEOF is returned by readLine when the reader reaches EOF.
var errElicitationEOF = errors.New("elicitation EOF")

// readLine reads a line from the reader, returning the trimmed line content.
// Returns errElicitationEOF if the reader reaches EOF.
func readLine(reader *bufio.Reader, context string) (string, error) {
	line, readErr := reader.ReadString('\n')
	if readErr != nil {
		if errors.Is(readErr, io.EOF) {
			return "", errElicitationEOF
		}

		return "", fmt.Errorf("reading %s: %w", context, readErr)
	}

	return strings.TrimSpace(line), nil
}

// promptElicitationAccept asks the user to accept or decline.
func promptElicitationAccept(reader *bufio.Reader, writer io.Writer) (copilot.ElicitationResult, error) {
	_, _ = fmt.Fprint(writer, "Accept? [y/N]: ")

	input, err := readLine(reader, "elicitation response")
	if err != nil {
		if errors.Is(err, errElicitationEOF) {
			return copilot.ElicitationResult{Action: "cancel"}, nil
		}

		return copilot.ElicitationResult{}, err
	}

	input = strings.ToLower(input)

	if input == "y" || input == "yes" {
		return copilot.ElicitationResult{Action: "accept"}, nil
	}

	return copilot.ElicitationResult{Action: "decline"}, nil
}

// promptElicitationFields prompts for each field in the schema.
// The user can type "!cancel" on any field to decline the elicitation.
func promptElicitationFields(
	reader *bufio.Reader, writer io.Writer, schema map[string]any,
) (copilot.ElicitationResult, error) {
	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		return promptElicitationAccept(reader, writer)
	}

	// Sort field names for deterministic prompt order
	fieldNames := make([]string, 0, len(props))
	for field := range props {
		fieldNames = append(fieldNames, field)
	}

	sort.Strings(fieldNames)

	_, _ = fmt.Fprintln(writer, "(type !cancel on any field to decline)")

	content := make(map[string]any, len(props))

	for _, field := range fieldNames {
		_, _ = fmt.Fprintf(writer, "%s: ", field)

		value, err := readLine(reader, "field "+field)
		if err != nil {
			if errors.Is(err, errElicitationEOF) {
				return copilot.ElicitationResult{Action: "cancel"}, nil
			}

			return copilot.ElicitationResult{}, err
		}

		if value == "!cancel" {
			return copilot.ElicitationResult{Action: "decline"}, nil
		}

		content[field] = value
	}

	return copilot.ElicitationResult{
		Action:  "accept",
		Content: content,
	}, nil
}
