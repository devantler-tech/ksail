package picker_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/picker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModel_Update_CtrlC(t *testing.T) {
	t.Parallel()

	mdl := picker.NewModel("Pick:", []string{"a", "b", "c"})

	updated, cmd := mdl.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	mdl = assertModel(t, updated)
	assert.True(t, mdl.Cancelled())
	assert.Empty(t, mdl.Selected())
	assert.NotNil(t, cmd)
}

func TestModel_Init(t *testing.T) {
	t.Parallel()

	mdl := picker.NewModel("Pick:", []string{"a"})

	cmd := mdl.Init()
	assert.Nil(t, cmd)
}

func TestModel_Update_ArrowKeys(t *testing.T) {
	t.Parallel()

	items := []string{"alpha", "beta", "gamma"}
	mdl := picker.NewModel("Pick:", items)

	// Move down with arrow key
	updated, _ := mdl.Update(tea.KeyMsg{Type: tea.KeyDown})
	mdl = assertModel(t, updated)
	assert.Equal(t, 1, mdl.Cursor())

	// Move up with arrow key
	updated, _ = mdl.Update(tea.KeyMsg{Type: tea.KeyUp})
	mdl = assertModel(t, updated)
	assert.Equal(t, 0, mdl.Cursor())
}

func TestNewModel_InitialState(t *testing.T) {
	t.Parallel()

	mdl := picker.NewModel("Select cluster:", []string{"dev", "staging", "prod"})

	assert.Equal(t, 0, mdl.Cursor())
	assert.Empty(t, mdl.Selected())
	assert.False(t, mdl.Cancelled())
}

func TestModel_Cancelled_AfterSelection(t *testing.T) {
	t.Parallel()

	mdl := picker.NewModel("Pick:", []string{"item1", "item2"})

	// Select first item
	updated, _ := mdl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mdl = assertModel(t, updated)

	assert.Equal(t, "item1", mdl.Selected())
	assert.False(t, mdl.Cancelled())
}

func TestModel_View_WithCursorOnSecondItem(t *testing.T) {
	t.Parallel()

	mdl := picker.NewModel("Choose:", []string{"option-a", "option-b"})

	// Move cursor to second item
	updated, _ := mdl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	mdl = assertModel(t, updated)

	view := mdl.View()
	assert.Contains(t, view, "Choose:")
	assert.Contains(t, view, "option-a")
	assert.Contains(t, view, "option-b")
	assert.Contains(t, view, "▸")
}

func TestModel_Update_SelectLastItem(t *testing.T) {
	t.Parallel()

	items := []string{"first", "second", "third"}
	mdl := picker.NewModel("Pick:", items)

	// Move to last item
	updated, _ := mdl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	mdl = assertModel(t, updated)
	updated, _ = mdl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	mdl = assertModel(t, updated)

	// Select it
	updated, cmd := mdl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mdl = assertModel(t, updated)
	assert.Equal(t, "third", mdl.Selected())
	assert.NotNil(t, cmd)
}

func TestRun_NilItems(t *testing.T) {
	t.Parallel()

	_, err := picker.Run("Pick:", nil)
	require.Error(t, err)
	require.ErrorIs(t, err, picker.ErrNoItems)
}

func TestModel_View_EmptyItems(t *testing.T) {
	t.Parallel()

	mdl := picker.NewModel("Empty picker:", nil)

	view := mdl.View()
	assert.Contains(t, view, "Empty picker:")
	assert.Contains(t, view, "esc/q cancel")
}

func TestErrVariables(t *testing.T) {
	t.Parallel()

	require.Error(t, picker.ErrCancelled)
	require.Error(t, picker.ErrNoItems)
	require.Error(t, picker.ErrUnexpectedModel)
	require.Error(t, picker.ErrNotInteractive)

	assert.Equal(t, "selection cancelled", picker.ErrCancelled.Error())
	assert.Equal(t, "no items to select from", picker.ErrNoItems.Error())
}
