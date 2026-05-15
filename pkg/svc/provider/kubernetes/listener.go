package kubernetes

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// minAddressParts is the minimum number of colon-separated parts in a host:port address.
const minAddressParts = 2

// localListener holds a TCP listener bound to localhost with a known port.
type localListener struct {
	Listener net.Listener
	Port     int
}

// newLocalListener creates a TCP listener on a random localhost port and
// extracts the assigned port number. On any error the listener is closed.
func newLocalListener(ctx context.Context) (*localListener, error) {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("create listener: %w", err)
	}

	addr := ln.Addr().String()

	parts := strings.Split(addr, ":")
	if len(parts) < minAddressParts {
		_ = ln.Close()

		return nil, fmt.Errorf("parse listener address %q: %w", addr, ErrUnexpectedAddressFormat)
	}

	port, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		_ = ln.Close()

		return nil, fmt.Errorf("parse listener port: %w", err)
	}

	return &localListener{Listener: ln, Port: port}, nil
}
