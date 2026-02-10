// Package configmanager provides centralized configuration management for KSail.
//
// This package contains interfaces and implementations for loading and managing
// configuration files across different distribution types (Kind, K3d, Talos, KSail),
// with support for environment variable overrides and field validation.
//
// Key functionality:
//   - ConfigManager interface for generic configuration loading
//   - Common helpers for loading, validation, and error formatting
//   - Support for default values when configuration files are missing
//
// Subpackages:
//   - k3d: K3d-specific configuration management
//   - kind: Kind-specific configuration management
//   - ksail: KSail-specific configuration management (import as ksailconfigmanager)
//   - loader: Configuration file loading utilities
//   - talos: Talos-specific configuration management
package configmanager
