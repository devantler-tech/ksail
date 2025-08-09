package color

import (
	"fmt"
)

// ANSI escape codes for colors
const (
	redColor    = "\033[31m"
	yellowColor = "\033[33m"
	resetColor  = "\033[0m"
	errorSymbol = "✗ "
)

// PrintError prints the given message in red color with a ✗ symbol.
func PrintError(format string, a ...interface{}) {
	fmt.Printf(redColor+errorSymbol+format+resetColor+"\n", a...)
}

// PrintWarning prints the given message in yellow color with a ✗ symbol.
func PrintWarning(format string, a ...interface{}) {
	fmt.Printf(yellowColor+errorSymbol+format+resetColor+"\n", a...)
}
