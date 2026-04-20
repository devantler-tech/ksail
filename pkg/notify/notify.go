package notify

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/timer"
	fcolor "github.com/fatih/color"
)

// Message type constants.
// Each type determines the message styling (color and symbol).
const (
	// ErrorType represents an error message (red, with ✗ symbol).
	ErrorType MessageType = iota
	// WarningType represents a warning message (yellow, with ⚠ symbol).
	WarningType
	// ActivityType represents an activity/progress message (default color, with ► symbol).
	ActivityType
	// GenerateType represents a file generation message (default color, with ✚ symbol).
	GenerateType
	// SuccessType represents a success message (green, with ✔ symbol).
	SuccessType
	// InfoType represents an informational message (blue, with ℹ symbol).
	InfoType
	// TitleType represents a title/header message (bold, with emoji (custom or default)).
	TitleType
)

// =============================================================================
// Message Types and Configuration
// =============================================================================

// MessageType defines the type of notification message.
type MessageType int

// Message represents a notification message to be displayed to the user.
type Message struct {
	// Type determines the message styling (color, symbol).
	Type MessageType
	// Content is the main message text to display.
	Content string
	// Timer is optional. If provided and the message type is SuccessType,
	// timing information will be printed in a separate block after the message.
	Timer timer.Timer
	// Emoji is used only for TitleType messages to customize the title icon.
	Emoji string
	// Writer is the output destination. If nil, defaults to os.Stdout.
	Writer io.Writer
	// Args are format arguments for Content if it contains format specifiers.
	Args []any
}

// =============================================================================
// Convenience Functions
// =============================================================================

// Errorf writes an error message to the writer.
func Errorf(writer io.Writer, format string, args ...any) {
	WriteMessage(Message{
		Type:    ErrorType,
		Content: format,
		Args:    args,
		Writer:  writer,
	})
}

// Warningf writes a warning message to the writer.
func Warningf(writer io.Writer, format string, args ...any) {
	WriteMessage(Message{
		Type:    WarningType,
		Content: format,
		Args:    args,
		Writer:  writer,
	})
}

// Activityf writes an activity/progress message to the writer.
func Activityf(writer io.Writer, format string, args ...any) {
	WriteMessage(Message{
		Type:    ActivityType,
		Content: format,
		Args:    args,
		Writer:  writer,
	})
}

// Generatef writes a file generation message to the writer.
func Generatef(writer io.Writer, format string, args ...any) {
	WriteMessage(Message{
		Type:    GenerateType,
		Content: format,
		Args:    args,
		Writer:  writer,
	})
}

// Successf writes a success message to the writer.
func Successf(writer io.Writer, format string, args ...any) {
	WriteMessage(Message{
		Type:    SuccessType,
		Content: format,
		Args:    args,
		Writer:  writer,
	})
}

// SuccessWithTimerf writes a success message with timing information to the writer.
func SuccessWithTimerf(writer io.Writer, tmr timer.Timer, format string, args ...any) {
	WriteMessage(Message{
		Type:    SuccessType,
		Content: format,
		Args:    args,
		Timer:   tmr,
		Writer:  writer,
	})
}

// Infof writes an informational message to the writer.
func Infof(writer io.Writer, format string, args ...any) {
	WriteMessage(Message{
		Type:    InfoType,
		Content: format,
		Args:    args,
		Writer:  writer,
	})
}

// Titlef writes a title/header message with an emoji to the writer.
func Titlef(writer io.Writer, emoji, format string, args ...any) {
	WriteMessage(Message{
		Type:    TitleType,
		Content: fmt.Sprintf(format, args...),
		Emoji:   emoji,
		Writer:  writer,
	})
}

// =============================================================================
// Core WriteMessage Function
// =============================================================================

// WriteMessage writes a formatted message based on the message configuration.
// It handles message styling, optional timing information, and proper output formatting.
//
// For simpler use cases, prefer the convenience functions: Errorf(), Warningf(),
// Successf(), Infof(), Activityf(), Generatef(), and Titlef().
//
// Note: Leading newlines between stages are handled automatically by StageSeparatingWriter.
// Wrap the output writer with NewStageSeparatingWriter() in command handlers to enable
// automatic stage separation.
func WriteMessage(msg Message) {
	// Default to stdout if no writer specified
	if msg.Writer == nil {
		msg.Writer = os.Stdout
	}

	// Format the message content
	content := msg.Content
	if len(msg.Args) > 0 {
		content = fmt.Sprintf(msg.Content, msg.Args...)
	}

	// Get message configuration based on type
	config := getMessageConfig(msg.Type)

	content = indentMultilineContent(content, config.indent)

	// Handle TitleType specially (uses emoji instead of symbol)
	if msg.Type == TitleType {
		emoji := msg.Emoji
		if emoji == "" {
			emoji = "ℹ️" // default emoji for titles
		}

		_, err := config.color.Fprintf(msg.Writer, "%s %s\n", emoji, content)
		handleNotifyError(err)

		return
	}

	// Write message with symbol and color
	_, err := config.color.Fprintf(msg.Writer, "%s%s\n", config.symbol, content)
	handleNotifyError(err)

	// Emit timing block only for success messages.
	// This preserves the existing success line unchanged and prints timing immediately after.
	if msg.Type == SuccessType && msg.Timer != nil {
		total, stage := msg.Timer.GetTiming()

		_, err = config.color.Fprintf(msg.Writer, "⏲ current: %s\n", stage.String())
		handleNotifyError(err)
		_, err = config.color.Fprintf(msg.Writer, "  total:  %s\n", total.String())
		handleNotifyError(err)
	}
}

// Message configuration helpers.

// messageConfig holds the styling configuration for each message type.
type messageConfig struct {
	symbol string
	// indent is the precomputed indentation string for aligning subsequent lines
	// of multi-line messages. It equals strings.Repeat(" ", len([]rune(symbol))).
	indent string
	color  *fcolor.Color
}

// Package-level color instances avoid allocating a new *fcolor.Color on every
// WriteMessage call. These objects are read-only after initialization (Fprintf
// does not mutate the Color), so sharing them across goroutines is safe.
//
//nolint:gochecknoglobals // cached color objects to avoid per-call allocations
var (
	colorError   = fcolor.New(fcolor.FgRed)
	colorWarning = fcolor.New(fcolor.FgYellow)
	colorDefault = fcolor.New(fcolor.Reset)
	colorSuccess = fcolor.New(fcolor.FgGreen)
	colorInfo    = fcolor.New(fcolor.FgBlue)
	colorTitle   = fcolor.New(fcolor.Reset, fcolor.Bold)
)

// symbolIndent is the shared precomputed indent for all message types whose
// symbol is exactly 2 runes wide (all typed symbols: "✗ ", "⚠ ", "► ", etc.).
const symbolIndent = "  "

// getMessageConfig returns the styling configuration for a given message type.
func getMessageConfig(msgType MessageType) messageConfig {
	switch msgType {
	case ErrorType:
		return messageConfig{symbol: "✗ ", indent: symbolIndent, color: colorError}
	case WarningType:
		return messageConfig{symbol: "⚠ ", indent: symbolIndent, color: colorWarning}
	case ActivityType:
		return messageConfig{symbol: "► ", indent: symbolIndent, color: colorDefault}
	case GenerateType:
		return messageConfig{symbol: "✚ ", indent: symbolIndent, color: colorDefault}
	case SuccessType:
		return messageConfig{symbol: "✔ ", indent: symbolIndent, color: colorSuccess}
	case InfoType:
		return messageConfig{symbol: "ℹ ", indent: symbolIndent, color: colorInfo}
	case TitleType:
		return messageConfig{symbol: "", indent: "", color: colorTitle}
	default:
		return messageConfig{symbol: "", indent: "", color: colorDefault}
	}
}

// Error handling helpers.

// handleNotifyError handles errors that occur during notification printing.
// Errors are logged to stderr rather than returned to avoid disrupting the user experience.
func handleNotifyError(err error) {
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "notify: failed to print message: %v\n", err)
	}
}

// Content formatting helpers.

// indentMultilineContent indents subsequent lines of multi-line content using the
// precomputed indent string from messageConfig. This ensures that multi-line messages
// are properly aligned with the first line's symbol.
func indentMultilineContent(content, indent string) string {
	if indent == "" || !strings.Contains(content, "\n") {
		return content
	}

	lines := strings.Split(content, "\n")

	for i := 1; i < len(lines); i++ {
		if lines[i] == "" {
			continue
		}

		lines[i] = indent + lines[i]
	}

	return strings.Join(lines, "\n")
}
