// Package mirrorregistry provides mirror registry setup and connection stages for cluster creation.
// It handles mirror registry provisioning, network creation, and containerd configuration
// for Kind, K3d, and Talos distributions.
//
// Note: This package handles mirror registries (pull-through caches for external registries).
// For local development registries, see the localregistry package.
package mirrorregistry
