// Package registry provides utilities for container registry mirror configuration.
//
// This package contains pure utility functions for parsing, validating, and generating
// configuration for container registry mirrors. These utilities are used by both the
// scaffolder (for generating initial configuration) and the registry provisioner
// (for managing runtime registry mirrors).
//
// Key functionality:
//   - ParseMirrorSpecs: Parse mirror specification strings
//   - BuildHostEndpointMap: Build host-to-endpoint mappings
//   - RenderK3dMirrorConfig: Generate K3d registry configuration
//   - GenerateScaffoldedHostsToml: Generate containerd hosts.toml content
//
// This package has no dependencies on other ksail packages and provides
// reusable registry configuration primitives.
package registry
