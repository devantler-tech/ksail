// Package clusterupdate provides shared types and helpers for cluster update
// operations. These are separated to avoid import cycles between provisioner
// implementations and the main provisioner interface package.
//
// It owns the Upgrader interface and its shared RecreationRequiredUpgrader embed
// (for distributions that reach a target version by recreation rather than an
// in-place upgrade), the update diff result types, and MergePersistedState — the
// single helper that merges non-introspectable persisted state (Talos ISO, local
// registry, mirrors directory) onto a baseline spec for every distribution's
// GetCurrentConfig.
package clusterupdate
