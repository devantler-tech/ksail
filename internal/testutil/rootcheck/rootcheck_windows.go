//go:build windows

// Package rootcheck provides small OS-aware helpers for permission-sensitive tests.
package rootcheck

// IsRootUser reports whether the current process is running as root.
func IsRootUser() bool {
	return false
}
