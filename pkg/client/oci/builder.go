package oci

import (
	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// Default naming constants.
const (
	defaultRepositoryName = "ksail-workloads"
	defaultArtifactName   = "ksail-workload"
)

// BuildOptions capture user-supplied inputs for building an OCI artifact from manifest directories.
//
// All fields are optional except SourcePath, RegistryEndpoint, and Version.
// Name and Repository default to source directory basename if not provided.
type BuildOptions struct {
	// Name is the artifact name (defaults to repository's last segment if empty).
	Name string
	// SourcePath is the directory containing Kubernetes manifest files (required).
	SourcePath string
	// RegistryEndpoint is the registry host:port (required, protocol prefixes are stripped).
	RegistryEndpoint string
	// Repository is the repository path (defaults to source directory basename if empty).
	Repository string
	// Version is the artifact tag (required, can be any non-empty string such as "dev", "latest", or a semantic version).
	Version string
	// Username is the optional username for registry authentication.
	// When provided with Password, enables basic authentication for the registry push.
	Username string
	// Password is the optional password for registry authentication.

	Password string
}

// ValidatedBuildOptions represents sanitized inputs ready for use by the builder implementation.
//
// All fields are guaranteed to be non-empty and properly formatted after validation.
type ValidatedBuildOptions struct {
	// Name is the normalized artifact name.
	Name string
	// SourcePath is the absolute path to the manifest directory.
	SourcePath string
	// RegistryEndpoint is the normalized registry host:port.
	RegistryEndpoint string
	// Repository is the normalized repository path.
	Repository string
	// Version is the validated version string.
	Version string
	// Username is the optional username for registry authentication.
	Username string
	// Password is the optional password for registry authentication.

	Password string
}

// BuildResult describes the outcome of a successful artifact build.
//
// Contains the complete artifact metadata including registry coordinates and timestamps.
type BuildResult struct {
	// Artifact contains the complete OCI artifact metadata after successful push.
	Artifact v1alpha1.OCIArtifact
}

// EmptyBuildOptions capture user-supplied inputs for building an empty OCI artifact.
//
// This is used when no source directory exists but an artifact reference is still required.
type EmptyBuildOptions struct {
	// Name is the artifact name (defaults to repository's last segment if empty).
	Name string
	// RegistryEndpoint is the registry host:port (required, protocol prefixes are stripped).
	RegistryEndpoint string
	// Repository is the repository path (required).
	Repository string
	// Version is the artifact tag (required, can be any non-empty string such as "dev", "latest", or a semantic version).
	Version string
	// Username is the optional username for registry authentication.
	Username string
	// Password is the optional password for registry authentication.

	Password string
}

// ValidatedEmptyBuildOptions represents sanitized inputs ready for building an empty artifact.
//
// All fields are guaranteed to be non-empty and properly formatted after validation.
type ValidatedEmptyBuildOptions struct {
	// Name is the normalized artifact name.
	Name string
	// RegistryEndpoint is the normalized registry host:port.
	RegistryEndpoint string
	// Repository is the normalized repository path.
	Repository string
	// Version is the validated version string.
	Version string
	// Username is the optional username for registry authentication.
	Username string
	// Password is the optional password for registry authentication.

	Password string
}
