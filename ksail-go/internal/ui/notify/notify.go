package notify

import (
	"fmt"
	"os"

	fcolor "github.com/fatih/color"
)

const (
	// Leading symbols for messages
	errorSymbol = "✗ "
	warnSymbol  = "⚠ "
)

// Errorf prints a red error message to stderr, prefixed with a symbol.
func Errorf(format string, a ...interface{}) {
	c := fcolor.New(fcolor.FgRed)
	c.Fprintf(os.Stderr, errorSymbol+format+"\n", a...)
}

// Error prints a red error message to stderr without a trailing newline, prefixed with a symbol.
func Error(a ...interface{}) {
	c := fcolor.New(fcolor.FgRed)
	c.Fprint(os.Stderr, errorSymbol)
	c.Fprint(os.Stderr, fmt.Sprint(a...))
}

// Errorln prints a red error message to stderr with a trailing newline, prefixed with a symbol.
func Errorln(a ...interface{}) {
	c := fcolor.New(fcolor.FgRed)
	c.Fprint(os.Stderr, errorSymbol)
	c.Fprintln(os.Stderr, fmt.Sprint(a...))
}

// Warnf prints a yellow high-focus message to stdout, prefixed with a symbol.
func Warnf(format string, a ...interface{}) {
	c := fcolor.New(fcolor.FgYellow)
	c.Printf(warnSymbol+format+"\n", a...)
}

// Warn prints a yellow high-focus message to stdout without a trailing newline, prefixed with a symbol.
func Warn(a ...interface{}) {
	c := fcolor.New(fcolor.FgYellow)
	c.Print(warnSymbol)
	c.Print(fmt.Sprint(a...))
}

// Warnln prints a yellow high-focus message to stdout with a trailing newline, prefixed with a symbol.
func Warnln(a ...interface{}) {
	c := fcolor.New(fcolor.FgYellow)
	c.Print(warnSymbol)
	c.Println(fmt.Sprint(a...))
}
