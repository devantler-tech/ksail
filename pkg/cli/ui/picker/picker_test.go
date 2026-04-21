package picker_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/picker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModel_Update_CursorMovement(t *testing.T) {
	t.Parallel()

	items := []string{"alpha", "beta", "gamma"}
	model := picker.NewModel("Pick one:", items)

	// Initial cursor is 0
	assert.Equal(t, 0, model.Cursor())

	// Move down
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = assertModel(t, updated)
	assert.Equal(t, 1, model.Cursor())

	// Move down again
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = assertModel(t, updated)
	assert.Equal(t, 2, model.Cursor())

	// Move down at bottom — stays at 2
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = assertModel(t, updated)
	assert.Equal(t, 2, model.Cursor())

	// Move up
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	model = assertModel(t, updated)
	assert.Equal(t, 1, model.Cursor())

	// Move up to top
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	model = assertModel(t, updated)
	assert.Equal(t, 0, model.Cursor())

	// Move up at top — stays at 0
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	model = assertModel(t, updated)
	assert.Equal(t, 0, model.Cursor())
}

func TestModel_Update_Selection(t *testing.T) {
	t.Parallel()

	items := []string{"alpha", "beta", "gamma"}
	model := picker.NewModel("Pick one:", items)

	// Move to "beta"
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model = assertModel(t, updated)

	// Press enter
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	assert.Equal(t, "beta", model.Selected())
	assert.False(t, model.Cancelled())
	assert.NotNil(t, cmd)
}

func TestModel_Update_CancelEsc(t *testing.T) {
	t.Parallel()

	mdl := picker.NewModel("Pick:", []string{"a", "b"})

	updated, cmd := mdl.Update(tea.KeyMsg{Type: tea.KeyEscape})
	mdl = assertModel(t, updated)
	assert.True(t, mdl.Cancelled())
	assert.Empty(t, mdl.Selected())
	assert.NotNil(t, cmd)
}

func TestModel_Update_CancelQ(t *testing.T) {
	t.Parallel()

	mdl := picker.NewModel("Pick:", []string{"a", "b"})

	updated, cmd := mdl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	mdl = assertModel(t, updated)
	assert.True(t, mdl.Cancelled())
	assert.NotNil(t, cmd)
}

func TestModel_Update_NonKeyMsg(t *testing.T) {
	t.Parallel()

	mdl := picker.NewModel("Pick:", []string{"a", "b"})

	// Send a non-key message (window size)
	updated, cmd := mdl.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	result := assertModel(t, updated)
	assert.Equal(t, 0, result.Cursor())
	assert.False(t, result.Cancelled())
	assert.Empty(t, result.Selected())
	assert.Nil(t, cmd)
}

func TestModel_View(t *testing.T) {
	t.Parallel()

	mdl := picker.NewModel("Select cluster:", []string{"dev", "staging"})
	view := mdl.View()

	assert.Contains(t, view, "Select cluster:")
	assert.Contains(t, view, "dev")
	assert.Contains(t, view, "staging")
	assert.Contains(t, view, "esc/q cancel")
}

func TestRun_EmptyItems(t *testing.T) {
	t.Parallel()

	_, err := picker.Run("Pick:", []string{})
	require.Error(t, err)
	require.ErrorIs(t, err, picker.ErrNoItems)
}

func TestModel_SelectFirstItem(t *testing.T) {
	t.Parallel()

	mdl := picker.NewModel("Pick:", []string{"only"})

	// Select immediately without moving
	updated, _ := mdl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mdl = assertModel(t, updated)
	assert.Equal(t, "only", mdl.Selected())
}

func TestModel_EnterWithEmptyItems(t *testing.T) {
	t.Parallel()

	mdl := picker.NewModel("Pick:", nil)

	// Pressing enter on an empty model should not panic
	updated, cmd := mdl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mdl = assertModel(t, updated)
	assert.Empty(t, mdl.Selected())
	assert.Nil(t, cmd)
}

// assertModel performs a checked type assertion from tea.Model to picker.Model.
func assertModel(t *testing.T, teaModel tea.Model) picker.Model {
	t.Helper()

	result, ok := teaModel.(picker.Model)
	require.True(t, ok, "expected picker.Model, got %T", teaModel)

	return result
}
