package color

import (
	"os"

	fcolor "github.com/fatih/color"
)

const (
	// Leading symbol for error/warning lines
	errorSymbol = "✗ "
)

// PrintError prints the given message in red color with a ✗ symbol.
func PrintError(format string, a ...interface{}) {
	c := fcolor.New(fcolor.FgRed)
	c.Fprintf(os.Stderr, errorSymbol+format+"\n", a...)
}

// PrintWarning prints the given message in yellow color with a ✗ symbol.
func PrintWarning(format string, a ...interface{}) {
	c := fcolor.New(fcolor.FgYellow)
	c.Fprintf(os.Stderr, errorSymbol+format+"\n", a...)
}
