package fsutil_test

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

const (
	windowsGOOS                 = "windows"
	windowsPrivilegeNotHeldCode = 1314
)

func skipWindowsSymlinkPrivilegeError(t *testing.T, err error) {
	t.Helper()

	if err != nil && runtime.GOOS == windowsGOOS &&
		(os.IsPermission(err) || isWindowsSymlinkPrivilegeError(err)) {
		t.Skip("skipping symlink test on Windows: creating symlinks requires additional privileges")
	}
}

func isWindowsSymlinkPrivilegeError(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		err = linkErr.Err
	}

	var errno syscall.Errno
	if errors.As(err, &errno) && errno == syscall.Errno(windowsPrivilegeNotHeldCode) {
		return true
	}

	return strings.Contains(
		strings.ToLower(err.Error()),
		"privilege not held",
	)
}
