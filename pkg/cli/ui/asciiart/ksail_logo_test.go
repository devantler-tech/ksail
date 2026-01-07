package asciiart_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/asciiart"
	"github.com/gkampitakis/go-snaps/snaps"
)

func TestMain(m *testing.M) {
	exitCode := m.Run()

	_, err := snaps.Clean(m, snaps.CleanOpts{Sort: true})
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to clean snapshots: " + err.Error() + "\n")

		os.Exit(1)
	}

	os.Exit(exitCode)
}

func TestPrintKSailLogo(t *testing.T) {
	t.Parallel()

	// Test snapshot first
	t.Run("snapshot", func(t *testing.T) {
		t.Parallel()

		var writer bytes.Buffer
		asciiart.PrintKSailLogo(&writer)
		snaps.MatchSnapshot(t, writer.String())
	})

	t.Run("produces_non_empty_output", testNonEmptyOutput)
	t.Run("contains_ascii_art_elements", testASCIIElements)
	t.Run("has_proper_line_structure", testLineStructure)
	t.Run("output_is_consistent", testOutputConsistency)
	t.Run("handles_different_writers", testDifferentWriters)
	t.Run("validates_output_format", testOutputFormat)
}

func testNonEmptyOutput(t *testing.T) {
	t.Helper()
	t.Parallel()

	var writer bytes.Buffer
	asciiart.PrintKSailLogo(&writer)

	output := writer.String()
	if len(output) == 0 {
		t.Error("Expected non-empty output")
	}
}

func testASCIIElements(t *testing.T) {
	t.Helper()
	t.Parallel()

	var writer bytes.Buffer
	asciiart.PrintKSailLogo(&writer)

	output := writer.String()

	// Verify presence of ASCII art characters
	expectedElements := []string{"__", "/", "\\", "|", "~", "^", "_", "-"}

	for _, element := range expectedElements {
		if !strings.Contains(output, element) {
			t.Errorf("Expected output to contain ASCII element %q", element)
		}
	}
}

func testLineStructure(t *testing.T) {
	t.Helper()
	t.Parallel()

	var writer bytes.Buffer
	asciiart.PrintKSailLogo(&writer)
	output := writer.String()

	// Verify multi-line structure
	lines := bytes.Split([]byte(output), []byte("\n"))
	if len(lines) < 8 {
		t.Errorf("Expected at least 8 lines of ASCII art, got %d", len(lines))
	}

	// Verify some lines have content (not all empty)
	nonEmptyLines := 0

	for _, line := range lines {
		if len(bytes.TrimSpace(line)) > 0 {
			nonEmptyLines++
		}
	}

	if nonEmptyLines < 5 {
		t.Errorf("Expected at least 5 non-empty lines, got %d", nonEmptyLines)
	}
}

func testOutputConsistency(t *testing.T) {
	t.Helper()
	t.Parallel()

	var writer1, writer2 bytes.Buffer

	asciiart.PrintKSailLogo(&writer1)
	asciiart.PrintKSailLogo(&writer2)

	if !bytes.Equal(writer1.Bytes(), writer2.Bytes()) {
		t.Error("Expected consistent output between multiple calls")
	}
}

func testDifferentWriters(t *testing.T) {
	t.Helper()
	t.Parallel()

	writers := []bytes.Buffer{
		{}, // Fresh buffer
		{}, // Another fresh buffer
	}

	for index, writer := range writers {
		w := writer
		asciiart.PrintKSailLogo(&w)

		output := w.String()
		if len(output) == 0 {
			t.Errorf("Writer %d produced empty output", index)
		}
	}
}

func testOutputFormat(t *testing.T) {
	t.Helper()
	t.Parallel()

	var writer bytes.Buffer
	asciiart.PrintKSailLogo(&writer)
	output := writer.String()

	// Should end with newline
	if !strings.HasSuffix(output, "\n") {
		t.Error("Expected output to end with newline")
	}

	// Should contain the KSail text pattern
	if !strings.Contains(output, "__") {
		t.Error("Expected output to contain KSail ASCII pattern")
	}
}

// TestPrintKSailLogo_Writers tests the function with various io.Writer implementations
// to ensure the public API works correctly with different output destinations.
func TestPrintKSailLogoWriters(t *testing.T) {
	t.Parallel()

	// Test with different writer scenarios
	testCases := []struct {
		name   string
		writer func() *bytes.Buffer
	}{
		{
			name: "fresh_buffer",
			writer: func() *bytes.Buffer {
				return &bytes.Buffer{}
			},
		},
		{
			name: "buffer_with_existing_content",
			writer: func() *bytes.Buffer {
				buf := &bytes.Buffer{}
				buf.WriteString("prefix_")

				return buf
			},
		},
		{
			name: "large_capacity_buffer",
			writer: func() *bytes.Buffer {
				buf := &bytes.Buffer{}
				buf.Grow(1000) // Pre-allocate space

				return buf
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			writer := testCase.writer()
			initialLen := writer.Len()

			asciiart.PrintKSailLogo(writer)

			// Verify content was written
			if writer.Len() <= initialLen {
				t.Error("Expected writer to have more content after PrintKSailLogo")
			}

			output := writer.String()

			// Extract just the logo part (after any prefix)
			logoOutput := output[initialLen:]
			if len(logoOutput) == 0 {
				t.Error("Expected logo output to be written to buffer")
			}
		})
	}
}

// TestPrintKSailLogo_BoundsHandling tests that the function handles lines
// of various lengths without panicking, demonstrating the robustness of
// the bounds checking implementation.
func TestPrintKSailLogoBoundsHandling(t *testing.T) {
	t.Parallel()

	var writer bytes.Buffer

	// This test verifies that the function doesn't panic on the actual logo content
	// which includes lines of different lengths that could trigger bounds issues
	// if proper length checking wasn't implemented.
	asciiart.PrintKSailLogo(&writer)

	output := writer.String()

	// Verify we got output without panics
	if len(output) == 0 {
		t.Error("Expected non-empty output from logo printing")
	}

	// Verify the output contains expected content
	if !strings.Contains(output, "KSail") && !strings.Contains(output, "__") {
		t.Error("Expected output to contain recognizable logo elements")
	}

	// Verify proper line structure (no panics means bounds checking worked)
	lines := strings.Split(output, "\n")
	if len(lines) < 5 {
		t.Errorf("Expected multiple lines in output, got %d", len(lines))
	}
}

// TestPrintKSailLogoEdgeCases tests edge cases in the color processing functions
// by using specially crafted logo content that exercises all code paths.
func TestPrintKSailLogoEdgeCases(t *testing.T) {
	t.Parallel()

	testCases := createEdgeCaseTestData()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var writer bytes.Buffer

			// Use reflection to access the internal function for testing edge cases
			// This allows us to test all code paths without modifying the embedded logo
			asciiart.PrintLogoFromString(&writer, testCase.logoContent)

			output := writer.String()

			// Verify output is non-empty and snapshot it
			if len(output) == 0 {
				t.Error("Expected non-empty output")
			}

			snaps.MatchSnapshot(t, output)
		})
	}
}

func createEdgeCaseTestData() []struct {
	name        string
	logoContent string
} {
	return []struct {
		name        string
		logoContent string
	}{
		{
			name: "short_lines_for_green_blue_cyan",
			logoContent: strings.Join([]string{
				"line0",                 // yellow
				"line1",                 // yellow
				"line2",                 // yellow
				"line3",                 // yellow
				"line4",                 // blue
				"short",                 // index 5: printGreenBlueCyanPart with line < 32 chars (5 chars)
				strings.Repeat("x", 37), // index 6: line == 37 chars
				"line7",                 // index 7: printGreenCyanPart with line < 32 chars
				"line8",                 // index 8: printGreenCyanPart with line < 32 chars
				"final",                 // default: blue
			}, "\n"),
		},
		{
			name: "various_length_lines",
			logoContent: strings.Join([]string{
				"yellow1",               // yellow
				"yellow2",               // yellow
				"yellow3",               // yellow
				"yellow4",               // yellow
				"blue_line",             // blue
				strings.Repeat("x", 31), // index 5: exactly 31 chars - edge case for < 32
				strings.Repeat("y", 37), // index 6: exactly 37 chars - edge case for == 37
				strings.Repeat("z", 30), // index 7: exactly 30 chars - edge case for < 32
				strings.Repeat("w", 35), // index 8: 35 chars
				"final_blue",            // default
			}, "\n"),
		},
		{
			name: "missing_coverage_edge_case",
			logoContent: strings.Join([]string{
				"y1", "y2", "y3", "y4", // yellow lines 0-3
				"blue", // blue line 4
				strings.Repeat(
					"a",
					35,
				), // index 5: 35 chars - should hit lineLen >= 32 && lineLen < 37 path
				strings.Repeat("b", 32), // index 6: exactly 32 chars - edge case
				"short7",                // index 7: short line for printGreenCyanPart
				"longer8",               // index 8: normal line for printGreenCyanPart
				"end",                   // final line
			}, "\n"),
		},
	}
}
