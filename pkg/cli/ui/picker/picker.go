// Package picker provides a reusable interactive list picker built on bubbletea.
// It presents a list of string items with arrow-key navigation and returns the user's selection.
package picker

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ErrCancelled is returned when the user cancels the picker (Esc, q, or Ctrl+C).
var ErrCancelled = errors.New("selection cancelled")

// ErrNoItems is returned when the picker is invoked with an empty item list.
var ErrNoItems = errors.New("no items to select from")

// ErrUnexpectedModel is returned when the bubbletea program returns an unexpected model type.
var ErrUnexpectedModel = errors.New("unexpected model type from picker")

// ErrNotInteractive is returned when stdin is not a terminal.
var ErrNotInteractive = errors.New(
	"interactive selection requires a terminal (pass the value as an argument instead)",
)

//nolint:gochecknoglobals // package-level styles are idiomatic for lipgloss
var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	normalStyle   = lipgloss.NewStyle()
)

// Model is the bubbletea model for the picker.
// Exported for unit testing of Update/View logic.
type Model struct {
	title     string
	items     []string
	cursor    int
	selected  string
	cancelled bool
}

// NewModel creates a picker model with the given title and items.
func NewModel(title string, items []string) Model {
	return Model{
		title: title,
		items: items,
	}
}

// Selected returns the selected item, or empty string if none selected.
func (m Model) Selected() string {
	return m.selected
}

// Cancelled returns true if the user cancelled the picker.
func (m Model) Cancelled() bool {
	return m.cancelled
}

// Cursor returns the current cursor position.
func (m Model) Cursor() int {
	return m.cursor
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "enter":
		if len(m.items) == 0 {
			return m, nil
		}

		m.selected = m.items[m.cursor]

		return m, tea.Quit
	case "esc", "q", "ctrl+c":
		m.cancelled = true

		return m, tea.Quit
	}

	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	var content strings.Builder

	content.WriteString(titleStyle.Render(m.title))
	content.WriteString("\n\n")

	for i, item := range m.items {
		if i == m.cursor {
			content.WriteString(cursorStyle.Render("▸ "))
			content.WriteString(selectedStyle.Render(item))
		} else {
			content.WriteString(normalStyle.Render("  " + item))
		}

		content.WriteString("\n")
	}

	content.WriteString("\n")
	content.WriteString(normalStyle.Render("↑/↓ navigate • enter select • esc/q cancel"))

	return content.String()
}

// Run displays an interactive picker with the given title and items,
// and returns the user's selected item. Returns ErrCancelled if the user
// cancels, ErrNoItems if the items slice is empty, or ErrNotInteractive
// if stdin is not a terminal.
func Run(title string, items []string) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("%w", ErrNoItems)
	}

	if !isTTY() {
		return "", fmt.Errorf("%w", ErrNotInteractive)
	}

	m := NewModel(title, items)

	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("picker program failed: %w", err)
	}

	final, ok := finalModel.(Model)
	if !ok {
		return "", fmt.Errorf("%w", ErrUnexpectedModel)
	}

	if final.cancelled {
		return "", fmt.Errorf("%w", ErrCancelled)
	}

	return final.selected, nil
}

// isTTY returns true if stdin is connected to a terminal.
func isTTY() bool {
	fileInfo, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}
