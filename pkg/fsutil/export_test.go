package fsutil

import "os"

// AtomicWriteFileWithRename exposes AtomicWriteFile with an injected rename so
// black-box tests can simulate a rename failure on any platform.
func AtomicWriteFileWithRename(
	path string,
	data []byte,
	perm os.FileMode,
	rename func(oldpath, newpath string) error,
) error {
	return atomicWriteFile(path, data, perm, rename)
}
