//go:build tools

// Package copilotbundledeps keeps dependencies of generated bundled-CLI code in
// the module graph before that ignored code exists. Otherwise the SDK bundler's
// tidy changes go.mod during release compilation and stamps binaries as dirty.
// This package is excluded from ordinary builds.
package copilotbundledeps

import _ "github.com/klauspost/compress/zstd"
