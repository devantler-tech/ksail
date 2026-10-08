// Package applecontainer implements provider.Provider for Apple's container runtime on macOS.
//
// The runtime exposes no socket or network API, so this provider drives the `container` CLI and
// reads the JSON it prints for `list` and `volume list`. Node containers and their volumes are
// found again through labels, never through naming conventions alone.
package applecontainer
