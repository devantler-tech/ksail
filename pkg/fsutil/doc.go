// Package fsutil provides utilities for filesystem operations.
//
// Key functionality:
//   - File reading: ReadFileSafe, FindFile
//   - File writing: TryWriteFile, AtomicWriteFile
//   - Path operations: ExpandHomePath, EvalCanonicalPath
//   - Path containment: IsPathWithinDirectory (the single symlink-escape guard)
//   - YAML helpers: SplitYAMLDocuments (lossy, read-only callers), IsYAMLFile
//   - Kubeconfig: UpdateKubeconfigFile (load-mutate-write with explicit options)
//
// Subpackages:
//   - configmanager: Configuration loading and management
//   - generator: Template and configuration generation
//   - marshaller: Serialization and deserialization
//   - scaffolder: Project scaffolding and file generation
//   - validator: Configuration validation
package fsutil
