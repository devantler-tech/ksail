package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// commandEvent is the Wails event the native menu emits for SPA actions (the writable counterpart to
// the navigation deep links). The SPA's useDesktopCommands hook subscribes to it.
const commandEvent = "ksail:command"

// viewMenuItem maps a menu label to the ksail:// deep link the SPA navigates to, with an accelerator.
// The items reuse the exact deep-link bridge already used for ksail:// URLs, so the Go side stays
// route-agnostic — the SPA owns navigation.
type viewMenuItem struct {
	label       string
	accelerator string
	url         string
}

// commandMenuItem maps a menu label to a ksail:command the SPA performs (refresh / create / theme).
type commandMenuItem struct {
	label       string
	accelerator string
	command     string
}

// installApplicationMenu adds a "View" menu (navigation + common actions) to the standard application
// menu. It starts from DefaultApplicationMenu so the standard App/Edit menus (and macOS clipboard
// shortcuts) are preserved. Navigation items reuse handleDeepLink (window.EmitEvent("ksail:open",
// url)); action items emit ksail:command — both delivered to the SPA over the existing Wails event
// bridge. An invalid accelerator is logged and ignored by Wails (the item still works via click).
func installApplicationMenu(app *application.App, window *application.WebviewWindow) {
	viewMenuItems := []viewMenuItem{
		{"Clusters", "CmdOrCtrl+1", "ksail://clusters"},
		{"Overview", "CmdOrCtrl+2", "ksail://overview"},
		{"Resources", "CmdOrCtrl+3", "ksail://resources"},
		{"Events", "CmdOrCtrl+4", "ksail://events"},
		{"Secrets", "CmdOrCtrl+5", "ksail://secrets"},
		{"Settings", "CmdOrCtrl+6", "ksail://settings"},
	}
	commandMenuItems := []commandMenuItem{
		{"Refresh", "CmdOrCtrl+R", "refresh"},
		{"New Cluster", "CmdOrCtrl+N", "new-cluster"},
		{"Toggle Theme", "CmdOrCtrl+Shift+L", "toggle-theme"},
	}

	menu := application.DefaultApplicationMenu()
	viewMenu := menu.AddSubmenu("View")

	for _, item := range viewMenuItems {
		url := item.url
		viewMenu.Add(item.label).
			SetAccelerator(item.accelerator).
			OnClick(func(_ *application.Context) { handleDeepLink(window, url) })
	}

	viewMenu.AddSeparator()

	for _, item := range commandMenuItems {
		command := item.command
		viewMenu.Add(item.label).
			SetAccelerator(item.accelerator).
			OnClick(func(_ *application.Context) { window.EmitEvent(commandEvent, command) })
	}

	app.Menu.SetApplicationMenu(menu)
}
