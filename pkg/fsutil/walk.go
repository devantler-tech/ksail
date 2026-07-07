package fsutil

import (
	"fmt"
	"io/fs"
	"path/filepath"
)

// WalkFiles returns the paths of all non-directory files under root for which keep
// returns true, in filesystem-walk order. root may be a single file. keep receives
// each candidate file's path and directory entry; returning an error aborts the
// walk and is propagated to the caller. This centralizes the WalkDir + skip-dirs +
// predicate boilerplate so callers only supply the file filter.
func WalkFiles(
	root string,
	keep func(path string, entry fs.DirEntry) (bool, error),
) ([]string, error) {
	var files []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		ok, keepErr := keep(path, entry)
		if keepErr != nil {
			return keepErr
		}

		if ok {
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}

	return files, nil
}
