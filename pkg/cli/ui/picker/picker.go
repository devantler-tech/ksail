// Package picker provides a reusable interactive list picker built on bubbletea.
// It presents a list of string items with arrow-key navigation and returns the user's selection.
// Type "/" to enter filter mode and narrow the list by keyword; Esc exits filter mode.
package picker

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui"
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
	filterStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

// Model is the bubbletea model for the picker.
// Exported for unit testing of Update/View logic.
type Model struct {
	title         string
	items         []string // original, unfiltered items
	filteredItems []string // items matching the current filter (nil when no filter)
	filter        string   // current filter query (only meaningful when filterActive)
	filterActive  bool     // true when filter mode is engaged (/ was pressed)
	cursor        int
	selected      string
	cancelled     bool
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

// FilterActive returns true when the picker is in filter mode.
func (m Model) FilterActive() bool {
	return m.filterActive
}

// Filter returns the current filter query string.
func (m Model) Filter() string {
	return m.filter
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

	visible := m.visibleItems()

	if m.filterActive {
		return m.updateFilterMode(keyMsg, visible)
	}

	return m.updateNormalMode(keyMsg, visible)
}

// View implements tea.Model.
func (m Model) View() string {
	var content strings.Builder

	content.WriteString(titleStyle.Render(m.title))
	content.WriteString("\n\n")

	if m.filterActive {
		content.WriteString(filterStyle.Render("Filter: " + m.filter + "_"))
		content.WriteString("\n\n")
	}

	visible := m.visibleItems()

	for i, item := range visible {
		if i == m.cursor {
			content.WriteString(cursorStyle.Render("▸ "))
			content.WriteString(selectedStyle.Render(item))
		} else {
			content.WriteString(normalStyle.Render("  " + item))
		}

		content.WriteString("\n")
	}

	if m.filterActive && len(visible) == 0 {
		content.WriteString(normalStyle.Render("  (no matches)"))
		content.WriteString("\n")
	}

	content.WriteString("\n")

	if m.filterActive {
		content.WriteString(normalStyle.Render("↑/↓ navigate • enter select • esc clear filter"))
	} else {
		content.WriteString(
			normalStyle.Render("↑/↓/j/k navigate • enter select • / filter • esc/q cancel"),
		)
	}

	return content.String()
}

// visibleItems returns the items currently shown (filtered or all).
func (m Model) visibleItems() []string {
	if m.filterActive && m.filteredItems != nil {
		return m.filteredItems
	}

	return m.items
}

func (m Model) updateFilterMode(
	keyMsg tea.KeyMsg,
	visible []string,
) (tea.Model, tea.Cmd) {
	switch keyMsg.String() {
	case "esc":
		return m.clearFilterMode(), nil
	case "backspace", "delete":
		return m.deleteFilterRune(), nil
	case "enter":
		return m.selectVisibleItem(visible)
	case "up":
		return m.moveCursorUp(), nil
	case "down":
		return m.moveCursorDown(len(visible)), nil
	case "ctrl+c":
		return m.cancel(), tea.Quit
	default:
		return m.appendFilterRunes(keyMsg.Runes), nil
	}
}

func (m Model) updateNormalMode(
	keyMsg tea.KeyMsg,
	visible []string,
) (tea.Model, tea.Cmd) {
	// Normal (non-filter) mode — vi-style and arrow-key navigation.
	switch keyMsg.String() {
	case "j", "down":
		if m.cursor < len(visible)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "enter":
		if len(visible) > 0 {
			m.selected = visible[m.cursor]

			return m, tea.Quit
		}
	case "esc", "q", "ctrl+c":
		m.cancelled = true

		return m, tea.Quit
	case "/":
		m.filterActive = true
		m.filter = ""
		m.filteredItems = nil
		m.cursor = 0
	}

	return m, nil
}

func (m Model) clearFilterMode() Model {
	m.filterActive = false
	m.filter = ""
	m.filteredItems = nil
	m.cursor = 0

	return m
}

func (m Model) deleteFilterRune() Model {
	if len(m.filter) == 0 {
		return m
	}

	runes := []rune(m.filter)
	m.filter = string(runes[:len(runes)-1])
	m.filteredItems = applyFilter(m.items, m.filter)
	m.cursor = 0

	return m
}

func (m Model) appendFilterRunes(runes []rune) Model {
	if len(runes) == 0 {
		return m
	}

	m.filter += string(runes)
	m.filteredItems = applyFilter(m.items, m.filter)
	m.cursor = 0

	return m
}

func (m Model) moveCursorUp() Model {
	if m.cursor > 0 {
		m.cursor--
	}

	return m
}

func (m Model) moveCursorDown(itemCount int) Model {
	if m.cursor < itemCount-1 {
		m.cursor++
	}

	return m
}

func (m Model) cancel() Model {
	m.cancelled = true

	return m
}

func (m Model) selectVisibleItem(
	visible []string,
) (tea.Model, tea.Cmd) {
	if len(visible) == 0 {
		return m, nil
	}

	m.selected = visible[m.cursor]

	return m, tea.Quit
}

func applyFilter(items []string, filter string) []string {
	if filter == "" {
		return nil
	}

	query := strings.ToLower(filter)
	result := make([]string, 0, len(items))

	for _, item := range items {
		if strings.Contains(strings.ToLower(item), query) {
			result = append(result, item)
		}
	}

	return result
}

// Run displays an interactive picker with the given title and items,
// and returns the user's selected item. Returns ErrCancelled if the user
// cancels, ErrNoItems if the items slice is empty, or ErrNotInteractive
// if stdin is not a terminal.
func Run(title string, items []string) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("%w", ErrNoItems)
	}

	if !ui.StdinIsTTY() {
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
