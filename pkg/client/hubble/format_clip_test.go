package hubble_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/hubble"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormatPlainLineClipsLongEndpoints verifies that an endpoint name longer
// than the fixed column width is truncated with an ellipsis so it cannot spill
// into the next column and break streaming-row alignment.
func TestFormatPlainLineClipsLongEndpoints(t *testing.T) {
	t.Parallel()

	longPod := strings.Repeat("a", 60)

	var out bytes.Buffer

	err := hubble.FormatPlainLine(&out, hubble.FlowRecord{
		Verdict:     "FORWARDED",
		Protocol:    "TCP",
		Source:      hubble.Endpoint{Namespace: "default", Pod: longPod},
		Destination: hubble.Endpoint{Namespace: "kube-system", Pod: "dns"},
	})
	require.NoError(t, err)

	line := out.String()
	assert.Contains(t, line, "…", "an overflowing endpoint name is ellipsized")
	assert.NotContains(t, line, longPod, "the full over-width name must not be emitted")
	// The 28-rune SOURCE column truncates to 27 runes + the ellipsis.
	assert.Contains(t, line, "default/"+strings.Repeat("a", 19)+"…")
}

// TestFormatPlainLineShortValuesPassThrough verifies that values shorter than
// the fixed column widths are written unmodified (no truncation or ellipsis).
func TestFormatPlainLineShortValuesPassThrough(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := hubble.FormatPlainLine(&out, hubble.FlowRecord{
		Verdict:     "FORWARDED",
		Protocol:    "TCP",
		Source:      hubble.Endpoint{Namespace: "default", Pod: "web"},
		Destination: hubble.Endpoint{Namespace: "kube-system", Pod: "dns"},
	})
	require.NoError(t, err)

	line := out.String()
	assert.Contains(t, line, "default/web")
	assert.Contains(t, line, "kube-system/dns")
	assert.Contains(t, line, "TCP")
	assert.Contains(t, line, "FORWARDED")
	assert.NotContains(t, line, "…", "short values must not be ellipsized")
}

// TestFormatPlainLineEmptyFieldsShowDash verifies that empty protocol, verdict,
// and endpoint fields are replaced with a dash rather than left blank.
func TestFormatPlainLineEmptyFieldsShowDash(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := hubble.FormatPlainLine(&out, hubble.FlowRecord{})
	require.NoError(t, err)

	line := out.String()
	// An empty record should show at least 3 dashes (source, destination,
	// protocol, verdict all fall back to "-").
	assert.GreaterOrEqual(t, strings.Count(line, "-"), 3)
}

// TestFormatPlainLineClipsLongProtocol verifies that a protocol string longer
// than the 8-rune PROTOCOL column is truncated with an ellipsis.
func TestFormatPlainLineClipsLongProtocol(t *testing.T) {
	t.Parallel()

	// 12 runes exceeds the 8-rune protocolColWidth.
	longProto := "VERYLONG-UDP"

	var out bytes.Buffer

	err := hubble.FormatPlainLine(&out, hubble.FlowRecord{
		Protocol:    longProto,
		Verdict:     "FORWARDED",
		Source:      hubble.Endpoint{Namespace: "ns", Pod: "p"},
		Destination: hubble.Endpoint{Namespace: "ns", Pod: "q"},
	})
	require.NoError(t, err)

	line := out.String()
	assert.Contains(t, line, "…", "a long protocol name must be ellipsized")
	assert.NotContains(t, line, longProto, "the full over-width protocol must not appear")
}

// TestFormatPlainLineNilTimeShowsDash verifies that a nil timestamp field
// renders as a dash rather than panicking or printing a zero-value time.
func TestFormatPlainLineNilTimeShowsDash(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := hubble.FormatPlainLine(&out, hubble.FlowRecord{
		Verdict: "DROPPED",
		// Time is nil (zero value for *time.Time).
	})
	require.NoError(t, err)

	line := out.String()
	// The time column falls back to "-" for a nil pointer.
	assert.NotContains(t, line, "0001-01-01", "nil time must not produce the zero Go time value")
}

// TestFormatPlainHeaderContainsAllColumns verifies that FormatPlainHeader writes
// all five column labels (TIME, SOURCE, DESTINATION, PROTOCOL, VERDICT) and ends
// with a newline, matching the layout of FormatPlainLine rows.
func TestFormatPlainHeaderContainsAllColumns(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := hubble.FormatPlainHeader(&out)
	require.NoError(t, err)

	header := out.String()
	assert.Contains(t, header, "TIME")
	assert.Contains(t, header, "SOURCE")
	assert.Contains(t, header, "DESTINATION")
	assert.Contains(t, header, "PROTOCOL")
	assert.Contains(t, header, "VERDICT")
	assert.True(t, strings.HasSuffix(header, "\n"), "header must end with a newline")
}

// TestFormatPlainHeaderIsOneLine verifies that FormatPlainHeader writes exactly
// one non-empty line, so a streaming consumer can print it once up front.
func TestFormatPlainHeaderIsOneLine(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	require.NoError(t, hubble.FormatPlainHeader(&out))

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	assert.Len(t, lines, 1, "FormatPlainHeader must produce exactly one line")
}

// TestFormatJSONLineSingleRecord verifies that FormatJSONLine writes a single
// valid JSON object followed by a newline, with no surrounding array brackets.
func TestFormatJSONLineSingleRecord(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := hubble.FormatJSONLine(&out, hubble.FlowRecord{
		Verdict:     "FORWARDED",
		Protocol:    "TCP",
		Source:      hubble.Endpoint{Namespace: "default", Pod: "web"},
		Destination: hubble.Endpoint{Namespace: "kube-system", Pod: "dns"},
	})
	require.NoError(t, err)

	raw := out.String()
	assert.True(t, strings.HasSuffix(raw, "\n"), "NDJSON line must end with a newline")
	assert.NotContains(t, raw, "[", "FormatJSONLine must not wrap output in an array")

	var record hubble.FlowRecord
	require.NoError(t, json.Unmarshal([]byte(strings.TrimRight(raw, "\n")), &record),
		"each line must be a valid JSON object")

	assert.Equal(t, "FORWARDED", record.Verdict)
	assert.Equal(t, "web", record.Source.Pod)
}

// TestFormatJSONLineMultipleCallsAreNDJSON verifies that calling FormatJSONLine
// multiple times writes one JSON object per line (NDJSON), never an array.
func TestFormatJSONLineMultipleCallsAreNDJSON(t *testing.T) {
	t.Parallel()

	records := []hubble.FlowRecord{
		{Verdict: "FORWARDED", Source: hubble.Endpoint{Pod: "web"}},
		{Verdict: "DROPPED", Source: hubble.Endpoint{Pod: "db"}},
	}

	var out bytes.Buffer

	for _, record := range records {
		require.NoError(t, hubble.FormatJSONLine(&out, record))
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	require.Len(t, lines, 2, "each FormatJSONLine call must emit exactly one line")

	for i, line := range lines {
		var record hubble.FlowRecord

		require.NoError(t, json.Unmarshal([]byte(line), &record), "line %d is not valid JSON", i)
	}
}

// TestFormatJSONLineOmitsNilTime verifies that a nil Time pointer is omitted
// from the JSON output (omitempty), not serialized as the zero time.
func TestFormatJSONLineOmitsNilTime(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := hubble.FormatJSONLine(&out, hubble.FlowRecord{
		Verdict: "DROPPED",
		Source:  hubble.Endpoint{Pod: "p"},
	})
	require.NoError(t, err)

	assert.NotContains(t, out.String(), "0001-01-01", "nil time must not produce the zero Go time")
	assert.NotContains(t, out.String(), `"time"`, "nil time field must be omitted from JSON")
}

// TestFormatJSONLinePreservesTime verifies that a non-nil Time field is included
// in the JSON output with the correct RFC3339 format.
func TestFormatJSONLinePreservesTime(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, time.June, 29, 10, 0, 0, 0, time.UTC)

	var out bytes.Buffer

	err := hubble.FormatJSONLine(&out, hubble.FlowRecord{
		Time:    &ts,
		Verdict: "FORWARDED",
	})
	require.NoError(t, err)

	assert.Contains(t, out.String(), "2026-06-29", "time field must appear when non-nil")
}
