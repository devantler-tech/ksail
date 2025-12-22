package registry

import "errors"

// Static errors for the registry package.
var (
	// ErrEmptyBaseDir is returned when the base directory is empty.
	ErrEmptyBaseDir = errors.New("baseDir cannot be empty")
)
