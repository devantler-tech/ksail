//go:build unix

// Package rootcheck provides small OS-aware helpers for permission-sensitive tests.
package rootcheck

import "os"

// IsRootUser reports whether the current process is running as root.
func IsRootUser() bool {
	return os.Geteuid() == 0
}
