package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RunServerWithTransport exposes runServer so tests can control the transport
// instead of serving over the process's standard input.
func RunServerWithTransport(
	ctx context.Context,
	cfg ServerConfig,
	transport mcpsdk.Transport,
) error {
	return runServer(ctx, cfg, transport)
}
