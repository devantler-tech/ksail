// Package compat provides compatibility with gRPC 1.67.0 and later.
// This package must be imported before any gRPC connections are made.
// See: https://github.com/siderolabs/talos/blob/main/cmd/talosctl/acompat/acompat.go
package compat

import "os"

// init is required to set the environment variable before any gRPC connections are made.
//
//nolint:gochecknoinits // init is required to set environment before gRPC is initialized
func init() {
	// Disable ALPN enforcement for compatibility with older Talos API servers.
	// Without this, gRPC 1.67.0+ will fail TLS handshakes with:
	// "transport: authentication handshake failed: EOF"
	//
	// See: https://github.com/grpc/grpc-go/pull/7564
	err := os.Setenv("GRPC_ENFORCE_ALPN_ENABLED", "false")
	if err != nil {
		panic(err)
	}
}
