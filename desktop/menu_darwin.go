//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// installEditMenu attaches a standard menu bar to the running NSApplication. The webview backend
// (github.com/webview/webview_go) creates a bare NSWindow with no menu, and on macOS the
// Cmd-C/Cmd-V/Cmd-X/Cmd-A clipboard shortcuts are delivered as menu key equivalents: with no Edit
// menu carrying the standard editing selectors there is nothing to route them to, so copy and paste
// silently do nothing inside the web view. Each item uses a nil target so the action travels the
// responder chain to the focused WKWebView, which implements cut:/copy:/paste:/selectAll:.
static void installEditMenu(void) {
	@autoreleasepool {
		NSApplication *app = [NSApplication sharedApplication];

		NSMenu *menubar = [[NSMenu alloc] init];
		[app setMainMenu:menubar];

		// Application menu — also wires up the conventional Hide/Quit shortcuts.
		NSMenuItem *appMenuItem = [[NSMenuItem alloc] init];
		[menubar addItem:appMenuItem];
		NSMenu *appMenu = [[NSMenu alloc] init];
		[appMenuItem setSubmenu:appMenu];
		[appMenu addItemWithTitle:@"Hide KSail" action:@selector(hide:) keyEquivalent:@"h"];
		[appMenu addItem:[NSMenuItem separatorItem]];
		[appMenu addItemWithTitle:@"Quit KSail" action:@selector(terminate:) keyEquivalent:@"q"];

		// Edit menu — enables clipboard and selection shortcuts inside the web view.
		NSMenuItem *editMenuItem = [[NSMenuItem alloc] init];
		[menubar addItem:editMenuItem];
		NSMenu *editMenu = [[NSMenu alloc] initWithTitle:@"Edit"];
		[editMenuItem setSubmenu:editMenu];
		[editMenu addItemWithTitle:@"Undo" action:@selector(undo:) keyEquivalent:@"z"];
		[editMenu addItemWithTitle:@"Redo" action:@selector(redo:) keyEquivalent:@"Z"];
		[editMenu addItem:[NSMenuItem separatorItem]];
		[editMenu addItemWithTitle:@"Cut" action:@selector(cut:) keyEquivalent:@"x"];
		[editMenu addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
		[editMenu addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];
		[editMenu addItem:[NSMenuItem separatorItem]];
		[editMenu addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
	}
}
// installEditMenu runs once at startup and the menu persists for the process lifetime.
*/
import "C"

// installNativeMenu installs the macOS application menu bar so clipboard and selection keyboard
// shortcuts work inside the web view. It must run on the main thread, alongside the other webview
// window setup calls.
func installNativeMenu() {
	C.installEditMenu()
}
