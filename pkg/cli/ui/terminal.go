package ui

import (
	"fmt"
	"os"
)

// SetTerminalTitle sets the terminal window title using ANSI escape sequences.
// This works on most modern terminals including Linux terminals, macOS Terminal/iTerm2,
// and Windows Terminal.
//
// The title will be visible in the terminal window's title bar.
//
// Example:
//
//	SetTerminalTitle("KSail - Cluster Management")
func SetTerminalTitle(title string) {
	// ANSI escape sequence: ESC ] 0 ; title BEL
	// \033]0; sets both icon name and window title
	// \007 is the BEL (bell) character that terminates the sequence
	fmt.Fprintf(os.Stdout, "\033]0;%s\007", title)
}
