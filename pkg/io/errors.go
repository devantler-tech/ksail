package io

import "errors"

// File operation errors.
var (
	// ErrPathOutsideBase is returned when a file path is outside the specified base directory.
	ErrPathOutsideBase = errors.New("invalid path: file is outside base directory")

	// ErrEmptyOutputPath is returned when the output path is empty.
	ErrEmptyOutputPath = errors.New("output path cannot be empty")

	// ErrBasePath is returned when the base path is empty.
	ErrBasePath = errors.New("base path cannot be empty")
)
