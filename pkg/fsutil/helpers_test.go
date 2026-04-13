package fsutil_test

import (
	"os"
	"runtime"
	"testing"
)

const windowsGOOS = "windows"

func skipWindowsSymlinkPrivilegeError(t *testing.T, err error) {
	t.Helper()

	if err != nil && runtime.GOOS == windowsGOOS && os.IsPermission(err) {
		t.Skip("skipping symlink test on Windows: creating symlinks requires additional privileges")
	}
}
