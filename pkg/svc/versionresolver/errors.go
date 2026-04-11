package versionresolver

import "errors"

// ErrNoVersionsFound is returned when no stable versions are available.
var ErrNoVersionsFound = errors.New("no stable versions found")

// ErrNoUpgradesAvailable is returned when no newer versions exist.
var ErrNoUpgradesAvailable = errors.New("no upgrades available")

// ErrInvalidVersion is returned when a version string cannot be parsed.
var ErrInvalidVersion = errors.New("invalid version")

// ErrRegistryAccess is returned when the OCI registry cannot be reached.
var ErrRegistryAccess = errors.New("failed to access registry")
