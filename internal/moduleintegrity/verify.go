// Package moduleintegrity verifies extracted Go module source against its pinned checksum.
package moduleintegrity

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/mod/sumdb/dirhash"
)

var (
	// ErrChecksumMismatch identifies source that differs from its authenticated archive.
	ErrChecksumMismatch = errors.New("module source checksum mismatch")
	// ErrInvalidTree identifies roots or entries that are not ordinary directories or files.
	ErrInvalidTree = errors.New("module source must contain only ordinary directories and files")
)

// Verify hashes every extracted file with Go's canonical module hash. Prefix is
// the authenticated module path and version, joined by "@". Cached .ziphash
// metadata is insufficient: the extracted directory can change independently.
func Verify(directory, prefix, checksum string) error {
	directory = filepath.Clean(directory)

	root, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect module root: %w", err)
	}

	if !root.IsDir() {
		return fmt.Errorf("%w: %s", ErrInvalidTree, directory)
	}

	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if !entry.IsDir() && !entry.Type().IsRegular() {
			return fmt.Errorf("%w: %s", ErrInvalidTree, path)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("inspect module source: %w", err)
	}

	actual, err := dirhash.HashDir(directory, prefix, dirhash.Hash1)
	if err != nil {
		return fmt.Errorf("hash module source: %w", err)
	}

	if actual != checksum {
		return fmt.Errorf("%w: got %s, want %s", ErrChecksumMismatch, actual, checksum)
	}

	return nil
}
