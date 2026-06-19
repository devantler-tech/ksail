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
	"context"
	_ "embed"
	"fmt"
	"log"
	"runtime"

	"github.com/devantler-tech/ksail/v7/pkg/cli/uiserver"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
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
	// run holds every deferred cleanup; main only translates a fatal startup error into a non-zero
	// exit AFTER run's defers (e.g. the watcher's context cancel) have run — calling log.Fatalf inside
	// run would skip them (gocritic exitAfterDefer).
	err := run()
	if err != nil {
		log.Fatalf("ksail-desktop: %v", err)
	}
}

// run wires up the desktop app and blocks until it shuts down, returning a fatal startup error rather
// than exiting so its deferred cleanup runs first.
func run() error {
	// Import the user's login-shell environment when launched from Finder/Dock/Spotlight (macOS
	// LaunchServices does not source shell profiles), so cluster providers that read HCLOUD_TOKEN,
	// OMNI_SERVICE_ACCOUNT_KEY, KUBECONFIG, PATH additions, etc. behave the same as a terminal launch.
	// No-op on other platforms and when the environment is already populated. Must run before
	// NewServer() builds the credential manager.
	hydrateLoginShellEnv()

	// The same configured server `ksail ui` uses (local cluster lifecycle, embedded SPA, credential
	// settings). Its Handler() is handed to the Wails AssetServer; we never bind a TCP listener.
	server := uiserver.NewServer()

	// Native notification + dock-badge services. Registered with the app (so their platform impls
	// start) and retained here so the cluster-status watcher can drive them. SendNotification/SetBadge
	// degrade gracefully where unsupported (e.g. an unsigned build, or a platform without a dock).
	notifSvc := notifications.New()
	dockSvc := dock.New()

	// window is assigned just below; the single-instance callback closes over it so a relayed deep link
	// can reach it. Declared first because the callback is part of the options passed to New().
	var window *application.WebviewWindow

	app := application.New(application.Options{
		Name:        windowTitle,
		Description: "Native desktop app to manage local Kubernetes clusters",
		Services: []application.Service{
			application.NewService(notifSvc),
			application.NewService(dockSvc),
		},
		// AssetServer in Handler-only mode: every webview request (the SPA at "/", /api/v1/*, and the
		// /api/v1/events SSE stream) is served by our handler. The asset server wraps the response
		// writer in a contentTypeSniffer that implements http.Flusher and, once a Content-Type is set,
		// passes writes straight through to the webview without buffering — so SSE streams live.
		Assets: application.AssetOptions{
			Handler: server.Handler(),
		},
		// Single instance: a second launch (e.g. opening a ksail:// link while the app is running)
		// relays its arguments to the running instance instead of starting a duplicate window. On macOS
		// the launched URL is captured and appended to the relayed Args (see deeplink.go + Wails v3).
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: appUniqueID,
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				url, _ := firstDeepLink(data.Args)
				handleDeepLink(window, url)
			},
		},
	})

	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  windowTitle,
		Width:  windowWidth,
		Height: windowHeight,
	})

	// Build on DefaultApplicationMenu (which provides the standard App/Edit menus — Cut/Copy/Paste/
	// Select All — wired to the webview) and add a "View" menu that navigates the SPA and runs common
	// actions through the same event bridge ksail:// deep links use. Keeping the default base preserves
	// macOS clipboard shortcuts; Windows/WebView2 and Linux/WebKitGTK handle clipboard natively.
	installApplicationMenu(app, window)

	// Restore the window to where it was last left (position + size) and keep persisting changes so it
	// reopens there next time; falls back to these centered defaults on first launch. See
	// trackWindowState.
	trackWindowState(app, window)

	// A cold launch via a ksail:// link (the app was not already running) delivers the URL through this
	// application event once the run loop is up; a launch while already running goes through the
	// SingleInstance relay configured above.
	app.Event.OnApplicationEvent(
		events.Common.ApplicationLaunchedWithUrl,
		func(event *application.ApplicationEvent) { handleDeepLink(window, event.Context().URL()) },
	)

	// This is a menu-bar app: closing the window (red button / ⌘W / menu Close) hides it to the tray
	// instead of destroying it, so it stays reopenable via the tray's "Show KSail". Must be registered
	// before Run(). See hideWindowOnClose.
	hideWindowOnClose(window)

	// A menu-bar/system-tray icon for quick access: show/hide the window or quit without going through
	// the Dock. Must be configured before Run() (which blocks until shutdown).
	installSystemTray(app, window)

	// Watch local cluster status for the lifetime of the app: a native notification when a cluster
	// reaches Ready/Failed, and an in-progress count on the dock badge. The context is cancelled when
	// Run returns (app quit), stopping the poll loop.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go watchClusterStatus(ctx, server.Service, notifSvc, dockSvc)

	err := app.Run()
	if err != nil {
		return fmt.Errorf("run desktop app: %w", err)
	}

	return nil
}

// installSystemTray adds a menu-bar/system-tray icon with show/hide/quit actions. It picks a platform-
// appropriate icon: a monochrome template glyph on macOS (auto-inverts for light/dark menu bars) and
// the full-color icon elsewhere (Windows' SetTemplateIcon is a no-op and Linux trays do not invert, so
// the alpha-only template would be near-invisible there).
func installSystemTray(app *application.App, window *application.WebviewWindow) {
	tray := app.SystemTray.New()

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
}

// windowHider is the subset of *application.WebviewWindow the close-to-tray hook needs. Declaring it as
// an interface lets the hook's behavior be unit-tested without a live Wails window (which needs a GUI).
type windowHider interface {
	Hide() application.Window
}

// hideWindowOnClose makes closing the window hide it to the menu bar/tray instead of destroying it, so
// the single window stays alive and reopenable from the tray's "Show KSail" for the app's lifetime.
// Quitting is explicit — the tray's "Quit KSail" (app.Quit) and ⌘Q both terminate the app process
// directly, bypassing this hook.
//
// Wails v3's built-in WindowClosing handler is a *listener* that destroys the window: it marks it
// destroyed, closes the native window, and removes it from the app's window registry. Once destroyed,
// the tray's window.Show() can only try to recreate the window (a no-op on this alpha, so "nothing
// happens"), and window.Hide() invokes hide on the freed native window, crashing the whole process —
// exactly the two reported tray failures. A *hook* runs before listeners and, when it cancels the
// event, short-circuits them (see WebviewWindow.HandleWindowEvent), so cancelling here skips the
// destroy and we hide instead. On macOS the native windowShouldClose: already returns false (it never
// closes the NSWindow itself; the Go listener did the destroying), so cancelling fully prevents it.
func hideWindowOnClose(window *application.WebviewWindow) {
	window.RegisterHook(events.Common.WindowClosing, hideOnCloseHook(window))
}

// hideOnCloseHook is the WindowClosing hook body: cancel the close (so Wails' destroying listener is
// skipped) then hide the window. Split from hideWindowOnClose so it can be unit-tested against a fake
// windowHider and a real application.WindowEvent.
func hideOnCloseHook(window windowHider) func(*application.WindowEvent) {
	return func(event *application.WindowEvent) {
		event.Cancel()
		window.Hide()
	}
}
