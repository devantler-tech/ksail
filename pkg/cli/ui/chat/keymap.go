package chat

import (
	"github.com/charmbracelet/bubbles/key"
)

// KeyMap defines all keybindings for the chat TUI.
// It implements the help.KeyMap interface for contextual help rendering.
type KeyMap struct {
	// Navigation
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding

	// Actions
	Send          key.Binding
	Queue         key.Binding
	DeletePending key.Binding
	Cancel        key.Binding
	Quit          key.Binding
	ToggleMode    key.Binding
	ToggleHelp    key.Binding

	// Tools
	ExpandTools key.Binding

	// Output
	CopyOutput key.Binding

	// YOLO mode
	ToggleYolo key.Binding

	// Modals
	OpenSessions key.Binding
	OpenModel    key.Binding
	NewChat      key.Binding

	// Permission modal
	Allow key.Binding
	Deny  key.Binding

	// Picker navigation
	Select key.Binding
	Rename key.Binding
	Delete key.Binding

	// Filter/search
	Filter key.Binding
}

// DefaultKeyMap returns the default keybindings.
func DefaultKeyMap() KeyMap {
	keyMap := KeyMap{}
	setNavigationKeys(&keyMap)
	setActionKeys(&keyMap)
	setToolAndOutputKeys(&keyMap)
	setModalKeys(&keyMap)
	setPickerAndFilterKeys(&keyMap)

	return keyMap
}

// ShortHelp returns keybindings for the mini help view (footer).
// This implements help.KeyMap interface.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Send,
		k.Queue,
		k.Up,
		k.PageUp,
		k.ToggleMode,
		k.OpenSessions,
		k.OpenModel,
		k.NewChat,
		k.Cancel,
		k.ToggleHelp,
	}
}

// FullHelp returns keybindings for the expanded help view (overlay).
// This implements help.KeyMap interface.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		// Column 1: Navigation
		{
			k.Send,
			k.Queue,
			k.DeletePending,
			k.Up,
			k.Down,
			k.PageUp,
			k.PageDown,
		},
		// Column 2: Mode & Tools
		{
			k.ToggleMode,
			k.ToggleYolo,
			k.ExpandTools,
			k.CopyOutput,
			k.ToggleHelp,
		},
		// Column 3: Modals & Session
		{
			k.OpenSessions,
			k.OpenModel,
			k.NewChat,
			k.Cancel,
			k.Quit,
		},
	}
}

// PermissionShortHelp returns keybindings for the permission modal.
func (k KeyMap) PermissionShortHelp() []key.Binding {
	return []key.Binding{k.Allow, k.Deny, k.Cancel}
}

// PickerShortHelp returns keybindings for picker modals.
func (k KeyMap) PickerShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Select, k.Cancel}
}

// SessionPickerShortHelp returns keybindings for the session picker.
func (k KeyMap) SessionPickerShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Select, k.Rename, k.Delete, k.Filter, k.Cancel}
}

func setNavigationKeys(keyMap *KeyMap) {
	keyMap.Up = key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("↑", "history up / navigate"),
	)
	keyMap.Down = key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("↓", "history down / navigate"),
	)
	keyMap.PageUp = key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("PgUp", "scroll up"),
	)
	keyMap.PageDown = key.NewBinding(
		key.WithKeys("pgdown"),
		key.WithHelp("PgDn", "scroll down"),
	)
}

func setActionKeys(keyMap *KeyMap) {
	keyMap.Send = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp(enterSymbol, "send message"),
	)
	keyMap.Queue = key.NewBinding(
		key.WithKeys("ctrl+q"),
		key.WithHelp("^Q", "queue prompt"),
	)
	keyMap.DeletePending = key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("Ctrl+D", "delete pending prompt"),
	)
	keyMap.Cancel = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel / quit"),
	)
	keyMap.Quit = key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("Ctrl+C", "force quit"),
	)
	keyMap.ToggleMode = key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("Tab", "cycle mode (agent/plan/ask)"),
	)
	keyMap.ToggleHelp = key.NewBinding(
		key.WithKeys("f1"),
		key.WithHelp("F1", "toggle help"),
	)
}

func setToolAndOutputKeys(keyMap *KeyMap) {
	keyMap.ExpandTools = key.NewBinding(
		key.WithKeys("ctrl+t"),
		key.WithHelp("Ctrl+T", "expand/collapse tools"),
	)
	keyMap.CopyOutput = key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("Ctrl+R", "copy latest output"),
	)
	keyMap.ToggleYolo = key.NewBinding(
		key.WithKeys("ctrl+y"),
		key.WithHelp("Ctrl+Y", "toggle auto-approve (YOLO)"),
	)
}

func setModalKeys(keyMap *KeyMap) {
	keyMap.OpenSessions = key.NewBinding(
		key.WithKeys("ctrl+h"),
		key.WithHelp("Ctrl+H", "session history"),
	)
	keyMap.OpenModel = key.NewBinding(
		key.WithKeys("ctrl+o"),
		key.WithHelp("Ctrl+O", "change model"),
	)
	keyMap.NewChat = key.NewBinding(
		key.WithKeys("ctrl+n"),
		key.WithHelp("Ctrl+N", "new chat"),
	)
	keyMap.Allow = key.NewBinding(
		key.WithKeys("y", "Y"),
		key.WithHelp("y", "allow"),
	)
	keyMap.Deny = key.NewBinding(
		key.WithKeys("n", "N"),
		key.WithHelp("n", "deny"),
	)
}

func setPickerAndFilterKeys(keyMap *KeyMap) {
	keyMap.Select = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("⏎", "select"),
	)
	keyMap.Rename = key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "rename"),
	)
	keyMap.Delete = key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "delete"),
	)
	keyMap.Filter = key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter"),
	)
}
