package oci

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	iohelpers "github.com/devantler-tech/ksail/v5/pkg/io"
)

// ErrRepositoryRequired indicates that the repository is required for empty artifacts.
var ErrRepositoryRequired = errors.New("repository is required")

// Validate normalizes and verifies the build options before artifact construction.
//
// This method performs the following validation steps:
//  1. Validates and resolves the source path to an absolute directory
//  2. Normalizes and validates the registry endpoint
//  3. Validates and normalizes the version (any non-empty string)
//  4. Normalizes repository and artifact names using source path defaults
//
// Returns ValidatedBuildOptions ready for use by the builder, or an error if validation fails.
func (o BuildOptions) Validate() (ValidatedBuildOptions, error) {
	trimmedSource := strings.TrimSpace(o.SourcePath)
	if trimmedSource == "" {
		return ValidatedBuildOptions{}, ErrSourcePathRequired
	}

	absSource, err := filepath.Abs(trimmedSource)
	if err != nil {
		return ValidatedBuildOptions{}, fmt.Errorf("resolve source path: %w", err)
	}

	info, statErr := os.Stat(absSource)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return ValidatedBuildOptions{}, ErrSourcePathNotFound
		}

		return ValidatedBuildOptions{}, fmt.Errorf("stat source path: %w", statErr)
	}

	if !info.IsDir() {
		return ValidatedBuildOptions{}, ErrSourcePathNotDirectory
	}

	endpoint, err := normalizeRegistryEndpoint(o.RegistryEndpoint)
	if err != nil {
		return ValidatedBuildOptions{}, err
	}

	version, err := normalizeVersion(o.Version)
	if err != nil {
		return ValidatedBuildOptions{}, err
	}

	repository := normalizeRepositoryName(o.Repository, absSource)
	name := normalizeArtifactName(o.Name, repository)

	return ValidatedBuildOptions{
		Name:             name,
		SourcePath:       absSource,
		RegistryEndpoint: endpoint,
		Repository:       repository,
		Version:          version,
		GitOpsEngine:     o.GitOpsEngine,
		Username:         o.Username,
		Password:         o.Password,
	}, nil
}

// Normalization helpers.

// normalizeRegistryEndpoint strips protocol prefixes and path suffixes from a registry endpoint.
// Returns the bare hostname:port portion suitable for OCI reference construction.
func normalizeRegistryEndpoint(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "oci://")
	trimmed = strings.TrimPrefix(trimmed, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	trimmed = strings.TrimSpace(trimmed)
	trimmed = strings.TrimSuffix(trimmed, "/")
	trimmed = strings.TrimSpace(trimmed)

	if trimmed == "" {
		return "", ErrRegistryEndpointRequired
	}

	if idx := strings.Index(trimmed, "/"); idx > 0 {
		trimmed = trimmed[:idx]
	}

	return trimmed, nil
}

// normalizeVersion validates and normalizes a version string.
// Accepts any non-empty string as a valid tag.
func normalizeVersion(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ErrVersionRequired
	}

	return trimmed, nil
}

// normalizeRepositoryName constructs a valid repository name from the candidate or source path.
// Falls back to source directory basename if candidate is empty.
// Sanitizes all path segments to lowercase alphanumeric with hyphens.
func normalizeRepositoryName(candidate, sourcePath string) string {
	pathCandidate := strings.TrimSpace(candidate)
	if pathCandidate == "" {
		pathCandidate = filepath.Base(sourcePath)
	}

	pathCandidate = filepath.ToSlash(pathCandidate)

	pathCandidate = strings.Trim(pathCandidate, "/")
	if pathCandidate == "" {
		pathCandidate = defaultRepositoryName
	}

	segments := strings.Split(pathCandidate, "/")

	normalized := make([]string, 0, len(segments))
	for _, segment := range segments {
		sanitized := sanitizeSegment(segment)
		if sanitized == "" {
			continue
		}

		normalized = append(normalized, sanitized)
	}

	if len(normalized) == 0 {
		return defaultRepositoryName
	}

	return strings.Join(normalized, "/")
}

// sanitizeSegment converts a repository path segment to lowercase alphanumeric with hyphens.
// Consecutive hyphens are collapsed to single hyphens.
// Leading and trailing hyphens are trimmed.
func sanitizeSegment(segment string) string {
	return iohelpers.SanitizeToDNSLabel(segment)
}

// normalizeArtifactName derives an artifact name from the candidate or repository.
// If candidate is empty, uses the last segment of the repository path.
// Sanitizes the result to lowercase alphanumeric with hyphens.
func normalizeArtifactName(candidate, repository string) string {
	trimmed := strings.TrimSpace(candidate)
	if trimmed == "" {
		parts := strings.Split(repository, "/")
		trimmed = parts[len(parts)-1]
	}

	normalized := sanitizeSegment(trimmed)
	if normalized == "" {
		return defaultArtifactName
	}

	return normalized
}

// Validate normalizes and verifies the empty build options before artifact construction.
//
// This method performs the following validation steps:
//  1. Normalizes and validates the registry endpoint
//  2. Validates that repository is provided (required for empty artifacts)
//  3. Validates and normalizes the version (any non-empty string)
//  4. Normalizes artifact name using repository
//
// Returns ValidatedEmptyBuildOptions ready for use by the builder, or an error if validation fails.
func (o EmptyBuildOptions) Validate() (ValidatedEmptyBuildOptions, error) {
	endpoint, err := normalizeRegistryEndpoint(o.RegistryEndpoint)
	if err != nil {
		return ValidatedEmptyBuildOptions{}, err
	}

	trimmedRepo := strings.TrimSpace(o.Repository)
	if trimmedRepo == "" {
		return ValidatedEmptyBuildOptions{}, ErrRepositoryRequired
	}

	repository := normalizeRepositoryName(trimmedRepo, "")

	version, err := normalizeVersion(o.Version)
	if err != nil {
		return ValidatedEmptyBuildOptions{}, err
	}

	name := normalizeArtifactName(o.Name, repository)

	return ValidatedEmptyBuildOptions{
		Name:             name,
		RegistryEndpoint: endpoint,
		Repository:       repository,
		Version:          version,
		GitOpsEngine:     o.GitOpsEngine,
		Username:         o.Username,
		Password:         o.Password,
	}, nil
}
