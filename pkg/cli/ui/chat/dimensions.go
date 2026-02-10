package chat

import tea "github.com/charmbracelet/bubbletea"

// handleWindowSize processes terminal resize events.
func (m *Model) handleWindowSize(msg tea.WindowSizeMsg) {
	m.width = msg.Width
	m.height = msg.Height
	m.updateDimensions()

	if !m.ready {
		m.ready = true
	}
}

// calculateMaxPickerVisible returns the maximum number of items that can be shown
// in a picker modal without pushing the viewport out of view.
func (m *Model) calculateMaxPickerVisible() int {
	// Calculate available height: total - header - input - footer - borders
	// Reserve space for: title (1) + scroll indicators (2) + borders (2) + minimum viewport
	availableHeight := m.height - headerHeight - inputHeight - footerHeight - viewportPadding - minViewportHeight

	// Subtract space for picker overhead (title + top/bottom padding)
	availableForItems := availableHeight - pickerOverhead

	// Calculate max items: cap between min and max
	maxItems := max(minPickerItems, min(availableForItems, maxPickerItems))

	return maxItems
}

// activeModalHeight returns the extra height needed for the currently active modal
// beyond the input area height (since modals replace the input area).
func (m *Model) activeModalHeight() int {
	switch {
	case m.showHelpOverlay:
		return 0
	case m.pendingPermission != nil:
		return m.permissionModalExtraHeight()
	case m.showModelPicker || m.showSessionPicker:
		return m.pickerModalExtraHeight()
	default:
		return 0
	}
}

// permissionModalExtraHeight calculates the extra height for the permission modal.
func (m *Model) permissionModalExtraHeight() int {
	contentLines := permissionBaseLines
	if m.pendingPermission.command != "" {
		contentLines++
	}

	if m.pendingPermission.arguments != "" {
		contentLines++
	}

	if contentLines > inputHeight {
		return contentLines - inputHeight
	}

	return 0
}

// pickerModalExtraHeight calculates the extra height for picker modals.
func (m *Model) pickerModalExtraHeight() int {
	var totalItems int
	if m.showSessionPicker {
		totalItems = len(m.filteredSessions) + 1 // +1 for "New Chat" option
	} else {
		totalItems = len(m.filteredModels) + 1 // +1 for "auto" option
	}

	maxVisible := m.calculateMaxPickerVisible()
	visibleCount := min(totalItems, maxVisible)
	isScrollable := totalItems > maxVisible

	// Calculate content lines: title + visible items
	contentLines := 1 + visibleCount
	if isScrollable {
		contentLines += scrollIndicatorLines // Add space for scroll indicators
	}

	// Apply minimum height constraint
	contentLines = max(contentLines, minPickerHeight)

	if contentLines > inputHeight {
		return contentLines - inputHeight
	}

	return 0
}

// updateDimensions updates component dimensions based on terminal size.
func (m *Model) updateDimensions() {
	// Account for borders and padding
	contentWidth := m.width - viewportPadding

	// Calculate available height: total - header - input - footer - borders - modal
	// Each bordered box adds 2 lines (top + bottom border)
	modalHeight := m.activeModalHeight()
	viewportHeight := max(
		m.height-headerHeight-inputHeight-footerHeight-viewportPadding-modalHeight,
		minHeight,
	)

	oldWidth := m.viewport.Width
	m.viewport.Width = contentWidth - viewportInner
	m.viewport.Height = viewportHeight
	m.textarea.SetWidth(contentWidth - viewportInner)

	// If viewport width changed, recreate the renderer and re-render completed messages
	if oldWidth != m.viewport.Width {
		m.renderer = createRenderer(m.viewport.Width - rendererMinWidth)
		m.reRenderCompletedMessages()
		m.updateViewportContent()
	}
}
