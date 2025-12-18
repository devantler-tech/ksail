package asciiart

import (
	_ "embed"
	"io"
	"strings"

	"github.com/fatih/color"
)

//go:embed ksail_logo.txt
var ksailLogo string

// Color segment split indices for logo rendering.
const (
	greenCyanSplitIndex = 32
	blueStartIndex      = 37
	cyanStartIndex      = 38
)

// PrintKSailLogo displays the KSail ASCII art with colored formatting.
func PrintKSailLogo(writer io.Writer) {
	PrintLogoFromString(writer, ksailLogo)
}

// PrintLogoFromString processes logo content and applies color formatting.
// This function is exposed for testing purposes to enable coverage of edge cases.
// In normal usage, use PrintKSailLogo instead.
func PrintLogoFromString(writer io.Writer, logoContent string) {
	const yellowLines = 4

	lines := strings.Split(logoContent, "\n")

	for index, line := range lines {
		switch {
		case index < yellowLines:
			printlnc(writer, colorYellow(), line)
		case index == yellowLines:
			printlnc(writer, colorBlue(), line)
		case index > yellowLines && index < 7:
			printGreenBlueCyanPart(writer, line)
		case index > 6 && index < len(lines)-2:
			printGreenCyanPart(writer, line)
		default:
			printlnc(writer, colorBlue(), line)
		}
	}
}

// Color printing helpers.

// printc writes a colored string to the output writer.
func printc(out io.Writer, c *color.Color, s string) { _, _ = c.Fprint(out, s) }

// printlnc writes a colored string followed by a newline to the output writer.
func printlnc(out io.Writer, c *color.Color, s string) { _, _ = c.Fprintln(out, s) }

// Color constructors.
// These functions create color instances on demand to avoid package-level globals.

func colorYellow() *color.Color { return color.New(color.Bold, color.FgYellow) }
func colorBlue() *color.Color   { return color.New(color.Bold, color.FgBlue) }
func colorGreen() *color.Color  { return color.New(color.Bold, color.FgGreen) }
func colorCyan() *color.Color   { return color.New(color.FgCyan) }

// Multi-segment color rendering helpers.

// printGreenBlueCyanPart renders a line with green, cyan, and blue color segments.
// Used for middle sections of the logo that require three-color rendering.
func printGreenBlueCyanPart(out io.Writer, line string) {
	lineLen := len(line)

	// Ensure we have enough characters for all segments
	if lineLen < greenCyanSplitIndex {
		printlnc(out, colorGreen(), line)

		return
	}

	// Print green segment
	printc(out, colorGreen(), line[:greenCyanSplitIndex])

	// Print cyan segment if we have enough characters
	if lineLen < blueStartIndex {
		printlnc(out, colorCyan(), line[greenCyanSplitIndex:])

		return
	}

	printc(out, colorCyan(), line[greenCyanSplitIndex:blueStartIndex])

	// Print blue segment if we have enough characters
	if lineLen < cyanStartIndex {
		printlnc(out, colorBlue(), line[blueStartIndex:])

		return
	}

	printc(out, colorBlue(), line[blueStartIndex:cyanStartIndex])

	// Print final cyan segment
	printlnc(out, colorCyan(), line[cyanStartIndex:])
}

// printGreenCyanPart renders a line with green and cyan color segments.
// Used for sections of the logo that require two-color rendering.
func printGreenCyanPart(out io.Writer, line string) {
	lineLen := len(line)

	// Ensure we have enough characters for the split
	if lineLen < greenCyanSplitIndex {
		printlnc(out, colorGreen(), line)

		return
	}

	// Print green segment
	printc(out, colorGreen(), line[:greenCyanSplitIndex])

	// Print cyan segment (remainder of the line)
	printlnc(out, colorCyan(), line[greenCyanSplitIndex:])
}
