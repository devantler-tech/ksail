//go:build !darwin

package main

// hydrateLoginShellEnv is a no-op on non-macOS platforms. The GUI-launch environment problem it
// addresses is specific to how macOS LaunchServices starts application bundles without sourcing the
// user's shell profile.
func hydrateLoginShellEnv() {}
