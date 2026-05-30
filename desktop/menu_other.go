//go:build !darwin

package main

// installNativeMenu is a no-op on platforms whose webview backend already provides clipboard
// shortcuts: WebView2 on Windows and WebKitGTK on Linux both handle Cut/Copy/Paste natively.
func installNativeMenu() {}
