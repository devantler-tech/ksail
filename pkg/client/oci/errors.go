package oci

import "errors"

// Build option validation errors.
var (
	// ErrSourcePathRequired indicates that no source path was provided in build options.
	ErrSourcePathRequired = errors.New("source path is required")
	// ErrSourcePathNotFound indicates that the provided source path does not exist.
	ErrSourcePathNotFound = errors.New("source path does not exist")
	// ErrSourcePathNotDirectory indicates that the provided source path is not a directory.
	ErrSourcePathNotDirectory = errors.New("source path must be a directory")
	// ErrRegistryEndpointRequired indicates that the registry endpoint is missing.
	ErrRegistryEndpointRequired = errors.New("registry endpoint is required")
	// ErrVersionRequired indicates that no semantic version was provided.
	ErrVersionRequired = errors.New("version is required")
	// ErrVersionInvalid indicates that the supplied version does not follow semantic versioning.
	ErrVersionInvalid = errors.New("version must follow semantic versioning")
	// ErrNoManifestFiles indicates that the source directory does not contain manifest files.
	ErrNoManifestFiles = errors.New("no manifest files found in source directory")
)
