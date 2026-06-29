package hubble

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"
)

// Tabwriter geometry for the plain-text flow table.
const (
	tabwriterPadding = 2
	tabwriterTabSize = 2
)

// FormatJSON writes records as an indented JSON array. An empty slice is
// rendered as `[]` so the output is always valid JSON.
func FormatJSON(out io.Writer, records []FlowRecord) error {
	if records == nil {
		records = []FlowRecord{}
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal flows to JSON: %w", err)
	}

	_, err = fmt.Fprintln(out, string(data))
	if err != nil {
		return fmt.Errorf("write JSON flows: %w", err)
	}

	return nil
}

// FormatPlain writes records as an aligned, human-readable table. When there
// are no records it writes a single explanatory line so the output is never
// silent.
func FormatPlain(out io.Writer, records []FlowRecord) error {
	if len(records) == 0 {
		_, err := fmt.Fprintln(out, "No flows observed.")
		if err != nil {
			return fmt.Errorf("write flows table: %w", err)
		}

		return nil
	}

	writer := tabwriter.NewWriter(out, 0, tabwriterTabSize, tabwriterPadding, ' ', 0)

	_, err := fmt.Fprintln(writer, "TIME\tSOURCE\tDESTINATION\tPROTOCOL\tVERDICT")
	if err != nil {
		return fmt.Errorf("write flows header: %w", err)
	}

	for _, record := range records {
		_, err = fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%s\t%s\n",
			formatTime(record.Time),
			endpointString(record.Source),
			endpointString(record.Destination),
			orDash(record.Protocol),
			orDash(record.Verdict),
		)
		if err != nil {
			return fmt.Errorf("write flows row: %w", err)
		}
	}

	err = writer.Flush()
	if err != nil {
		return fmt.Errorf("flush flows table: %w", err)
	}

	return nil
}

// endpointString renders an endpoint as "namespace/pod", falling back to
// whichever part is present, or "-" when neither is.
func endpointString(endpoint Endpoint) string {
	switch {
	case endpoint.Namespace != "" && endpoint.Pod != "":
		return endpoint.Namespace + "/" + endpoint.Pod
	case endpoint.Pod != "":
		return endpoint.Pod
	case endpoint.Namespace != "":
		return endpoint.Namespace
	default:
		return "-"
	}
}

func formatTime(timestamp time.Time) string {
	if timestamp.IsZero() {
		return "-"
	}

	return timestamp.UTC().Format(time.RFC3339)
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}

	return value
}
