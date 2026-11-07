//go:build !darwin

package caskcompletion

import "os/exec"

type terminalGuard struct{}

func newTerminalGuard(_ *exec.Cmd) *terminalGuard { return &terminalGuard{} }
func (*terminalGuard) identify() error            { return ErrUnsupportedHost }
func (*terminalGuard) capture() error             { return ErrUnsupportedHost }
func (*terminalGuard) stop() error                { return nil }
