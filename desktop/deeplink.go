package main

import (
	"strings"

	//nolint:depguard // wails is the desktop app's UI runtime; the CLI module's allowlist does not apply here.
	"github.com/wailsapp/wails/v3/pkg/application"
)

// appUniqueID identifies the running instance for single-instance enforcement. It matches the macOS
// bundle identifier so a second launch (including a ksail:// URL launch) relays to the first instance
// rather than starting a duplicate.
const appUniqueID = "tech.devantler.ksail.desktop"

// deepLinkScheme is the custom URL scheme registered in the bundle (Info.plist CFBundleURLTypes). A
// ksail://... link opens or focuses the app and asks the SPA to navigate to the linked view.
const deepLinkScheme = "ksail"

// deepLinkEvent is the Wails event the Go side emits to the SPA carrying a received deep-link URL. The
// SPA — only when running inside the Wails webview — subscribes and routes to the target view.
const deepLinkEvent = "ksail:open"

// firstDeepLink returns the first ksail:// URL among the args, if any. macOS does not put URL-scheme
// launches in argv; Wails v3's captureLaunchURL appends the captured URL to SecondInstanceData.Args so
// it surfaces the same way it does on Windows/Linux (which pass it through argv natively).
func firstDeepLink(args []string) (string, bool) {
	for _, arg := range args {
		if isDeepLink(arg) {
			return arg, true
		}
	}

	return "", false
}

// isDeepLink reports whether raw is a ksail:// URL.
func isDeepLink(raw string) bool {
	return strings.HasPrefix(raw, deepLinkScheme+"://")
}

// handleDeepLink relays a received ksail:// URL to the SPA and brings the window to the front. The SPA
// owns the actual navigation, so the Go side stays route-agnostic: it forwards the raw URL and shows
// the window. A non-ksail or empty URL just focuses the window (a bare relaunch).
func handleDeepLink(window *application.WebviewWindow, raw string) {
	if window == nil {
		return
	}

	if isDeepLink(raw) {
		window.EmitEvent(deepLinkEvent, raw)
	}

	window.Show()
	window.Restore()
	window.Focus()
}
