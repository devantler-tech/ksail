package fsutil

import "sync"

// ResetHomeDirCache resets the cached home directory, allowing tests to exercise
// the first-call path of currentHomeDir. This is the export_test.go seam pattern.
//
// IMPORTANT: This function is NOT safe for concurrent use. It must only be called
// from non-parallel tests (i.e., tests that do not use t.Parallel() / b.SetParallelism),
// and no other goroutine may call ExpandHomePath or currentHomeDir concurrently.
//
//nolint:gochecknoglobals // Standard export_test.go seam.
var ResetHomeDirCache = func() {
	homeDirOnce = sync.Once{}
	homeDirValue = ""
	errHomeDir = nil
}
