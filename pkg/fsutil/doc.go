// Package fsutil provides utilities for input and output operations.
//
// This package contains utilities for reading from and writing to files,
// along with various I/O helper functions for file operations, including
// configuration validation utilities.
//
// Key functionality:
//   - File reading: ReadFileSafe, FindFile
//   - File writing: TryWriteFile
//   - Path operations: ExpandHomePath
//   - String utilities: TrimNonEmpty
//
// Subpackages:
//   - configmanager: Configuration loading and management
//   - generator: Template and configuration generation
//   - marshaller: Serialization and deserialization
//   - scaffolder: Project scaffolding and file generation
//   - validator: Configuration validation
package fsutil
