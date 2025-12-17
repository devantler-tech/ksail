// Package registry centralizes all registry lifecycle helpers that KSail-Go
// needs for local development clusters.
//
// The package includes:
//   - mirror registry utilities used by cluster provisioners (Kind, K3d) to
//     handle pull-through caching containers consistently, and
//   - the developer-facing registry service abstraction that provisions the
//     localhost-only OCI registry leveraged by the CLI.
package registry
