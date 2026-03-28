// Package picker provides a reusable interactive list picker built on bubbletea.
// It presents a list of string items with arrow-key navigation and returns the user's selection.
package picker

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ErrCancelled is returned when the user cancels the picker (Esc or q).
var ErrCancelled = errors.New("selection cancelled")

// ErrNoItems is returned when the picker is invoked with an empty item list.
var ErrNoItems = errors.New("no items to select from")

// ErrUnexpectedModel is returned when the bubbletea program returns an unexpected model type.
var ErrUnexpectedModel = errors.New("unexpected model type from picker")

//nolint:gochecknoglobals // package-level styles are idiomatic for lipgloss
var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	normalStyle   = lipgloss.NewStyle()
)

type model struct {
	title     string
	items     []string
	cursor    int
	selected  string
	cancelled bool
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		m.selected = m.items[m.cursor]

		return m, tea.Quit
	case "esc", "q", "ctrl+c":
		m.cancelled = true

		return m, tea.Quit
	}

	return m, nil
}

func (m model) View() string {
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
	content.WriteString(normalStyle.Render("↑/↓ navigate • enter select • esc cancel"))

	return content.String()
}

// Run displays an interactive picker with the given title and items,
// and returns the user's selected item. Returns ErrCancelled if the user
// presses Esc/q, or ErrNoItems if the items slice is empty.
func Run(title string, items []string) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("%w", ErrNoItems)
	}

	m := model{
		title: title,
		items: items,
	}

	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("picker program failed: %w", err)
	}

	final, ok := finalModel.(model)
	if !ok {
		return "", fmt.Errorf("%w", ErrUnexpectedModel)
	}

	if final.cancelled {
		return "", fmt.Errorf("%w", ErrCancelled)
	}

	return final.selected, nil
}
