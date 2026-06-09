// Command ksail-desktop runs the KSail web UI in a native desktop window using Wails v3.
//
// It reuses the same in-process server as `ksail ui` (pkg/cli/uiserver): NewServer().Handler() is an
// http.Handler that serves the embedded SPA plus the REST API + SSE backed by the local cluster
// lifecycle. That handler is given to the Wails webview as its AssetServer handler, so the SPA, the
// REST endpoints, and the Server-Sent Events stream are all served same-origin (at the wails://wails
// production origin) with no loopback TCP port, no CORS, and no SPA changes — the same SPA the
// operator and `ksail ui` serve in a browser.
//
// This is a separate Go module so its CGO/webview dependency stays out of the main, statically linked
// `ksail` binary.
package main

import (
	_ "embed"
	"log"
	"runtime"

	"github.com/devantler-tech/ksail/v7/pkg/cli/uiserver"

	//nolint:depguard // wails is the desktop app's UI runtime; the CLI module's allowlist does not apply here.
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	windowTitle  = "KSail"
	windowWidth  = 1100
	windowHeight = 820
)

// trayIcon is the full-color app icon, used for the Windows/Linux system tray (their trays render a
// color image, not a monochrome template). Embedded so the single binary needs no external asset.
//
//go:embed resources/icon.png
var trayIcon []byte

// menuBarIcon is the macOS menu-bar glyph: a monochrome, alpha-only silhouette of the twin-sail
// sloop mark (no badge tile), generated from resources/menubar-icon.svg. macOS treats it as a
// template image (NSImage setTemplate:YES) and auto-inverts it for light/dark menu bars. It is a
// high-res square PNG that Wails scales down to the status-bar thickness.
//
//go:embed resources/menubar-icon.png
var menuBarIcon []byte

func main() {
	// Import the user's login-shell environment when launched from Finder/Dock/Spotlight (macOS
	// LaunchServices does not source shell profiles), so cluster providers that read HCLOUD_TOKEN,
	// OMNI_SERVICE_ACCOUNT_KEY, KUBECONFIG, PATH additions, etc. behave the same as a terminal launch.
	// No-op on other platforms and when the environment is already populated. Must run before
	// NewServer() builds the credential manager.
	hydrateLoginShellEnv()

	// The same configured server `ksail ui` uses (local cluster lifecycle, embedded SPA, credential
	// settings). Its Handler() is handed to the Wails AssetServer; we never bind a TCP listener.
	server := uiserver.NewServer()

	app := application.New(application.Options{
		Name:        windowTitle,
		Description: "Native desktop app to manage local Kubernetes clusters",
		// AssetServer in Handler-only mode: every webview request (the SPA at "/", /api/v1/*, and the
		// /api/v1/events SSE stream) is served by our handler. The asset server wraps the response
		// writer in a contentTypeSniffer that implements http.Flusher and, once a Content-Type is set,
		// passes writes straight through to the webview without buffering — so SSE streams live.
		Assets: application.AssetOptions{
			Handler: server.Handler(),
		},
	})

	// The application menu is intentionally left unset: macOS applies DefaultApplicationMenu() (which
	// includes the standard Edit menu — Cut/Copy/Paste/Select All — wired to the webview), replacing
	// the hand-rolled Cocoa menu the previous webview_go shell required. Windows/WebView2 and
	// Linux/WebKitGTK handle clipboard shortcuts natively.

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  windowTitle,
		Width:  windowWidth,
		Height: windowHeight,
	})

	// Restore the window to where it was last left (position + size) and keep persisting changes so it
	// reopens there next time; falls back to these centered defaults on first launch. See
	// trackWindowState.
	trackWindowState(app, window)

	// A menu-bar/system-tray icon for quick access: show/hide the window or quit without going through
	// the Dock. Must be configured before Run() (which blocks until shutdown).
	tray := app.SystemTray.New()
	// macOS: use a monochrome template image so the glyph auto-inverts for light/dark menu bars (the
	// native idiom). Elsewhere use the full-color icon: Windows' SetTemplateIcon is a no-op (it would
	// leave the tray blank) and Linux trays render the image as-is with no auto-inversion, so the
	// alpha-only template would be near-invisible there.
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(menuBarIcon)
	} else {
		tray.SetIcon(trayIcon)
	}

	trayMenu := app.NewMenu()
	trayMenu.Add("Show KSail").OnClick(func(_ *application.Context) { window.Show() })
	trayMenu.Add("Hide KSail").OnClick(func(_ *application.Context) { window.Hide() })
	trayMenu.AddSeparator()
	trayMenu.Add("Quit KSail").OnClick(func(_ *application.Context) { app.Quit() })
	tray.SetMenu(trayMenu)

	err := app.Run()
	if err != nil {
		log.Fatalf("ksail-desktop: %v", err)
	}
}
