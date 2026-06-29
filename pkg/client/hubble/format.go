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

// Fixed column widths for the streaming plain output. A [tabwriter] needs every
// row up front to align, which an unbounded follow stream never provides, so
// [FormatPlainHeader] and [FormatPlainLine] use fixed widths to keep live rows
// readable and aligned with the header.
const (
	timeColWidth     = 30
	endpointColWidth = 28
	protocolColWidth = 8
)

// streamRowFormat lays out one streaming row: time, source, destination,
// protocol (all left-padded to fixed widths), then the trailing verdict.
const streamRowFormat = "%-*s %-*s %-*s %-*s %s\n"

// clip truncates s to at most width runes, replacing the overflowing tail with an
// ellipsis so a long value (e.g. a generated pod name) cannot spill past its
// column and break the alignment of the fixed-width streaming rows. The padding
// verb (%-*s) only pads short values; it never truncates long ones, hence this.
// width <= 0 returns s unchanged.
func clip(value string, width int) string {
	if width <= 0 {
		return value
	}

	runes := []rune(value)
	if len(runes) <= width {
		return value
	}

	if width == 1 {
		return "…"
	}

	return string(runes[:width-1]) + "…"
}

// FormatPlainHeader writes the streaming table's header row once, before any
// flows. It mirrors the columns of [FormatPlainLine].
func FormatPlainHeader(out io.Writer) error {
	_, err := fmt.Fprintf(
		out,
		streamRowFormat,
		timeColWidth, "TIME",
		endpointColWidth, "SOURCE",
		endpointColWidth, "DESTINATION",
		protocolColWidth, "PROTOCOL",
		"VERDICT",
	)
	if err != nil {
		return fmt.Errorf("write flows header: %w", err)
	}

	return nil
}

// FormatPlainLine writes a single flow as one fixed-width row, for streaming
// output where rows arrive one at a time.
func FormatPlainLine(out io.Writer, record FlowRecord) error {
	_, err := fmt.Fprintf(
		out,
		streamRowFormat,
		timeColWidth, clip(formatTime(record.Time), timeColWidth),
		endpointColWidth, clip(endpointString(record.Source), endpointColWidth),
		endpointColWidth, clip(endpointString(record.Destination), endpointColWidth),
		protocolColWidth, clip(orDash(record.Protocol), protocolColWidth),
		orDash(record.Verdict),
	)
	if err != nil {
		return fmt.Errorf("write flow row: %w", err)
	}

	return nil
}

// FormatJSONLine writes a single flow as one line of newline-delimited JSON
// (NDJSON), the streaming counterpart to the [FormatJSON] array.
func FormatJSONLine(out io.Writer, record FlowRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal flow to JSON: %w", err)
	}

	_, err = fmt.Fprintln(out, string(data))
	if err != nil {
		return fmt.Errorf("write JSON flow: %w", err)
	}

	return nil
}

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

func formatTime(timestamp *time.Time) string {
	if timestamp == nil || timestamp.IsZero() {
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
