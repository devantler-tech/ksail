package fsutil

import "sync"

// ResetHomeDirCache resets the cached home directory, allowing tests to exercise
// the first-call path of currentHomeDir. This is the export_test.go seam pattern.
//
//nolint:gochecknoglobals // Standard export_test.go seam.
var ResetHomeDirCache = func() {
	homeDirOnce = sync.Once{}
	homeDirValue = ""
	homeDirErr = nil
}
