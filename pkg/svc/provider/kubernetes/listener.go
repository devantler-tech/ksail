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

// newLocalListener creates a TCP listener on the given localhost port and extracts the
// assigned port number. A localPort of 0 binds a random free port; a non-zero value binds
// that specific port (used to make a port-forward reachable at a stable, known address).
// On any error the listener is closed.
func newLocalListener(ctx context.Context, localPort int) (*localListener, error) {
	listener, err := (&net.ListenConfig{}).Listen(
		ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", localPort),
	)
	if err != nil {
		return nil, fmt.Errorf("create listener: %w", err)
	}

	addr := listener.Addr().String()

	parts := strings.Split(addr, ":")
	if len(parts) < minAddressParts {
		_ = listener.Close()

		return nil, fmt.Errorf("parse listener address %q: %w", addr, ErrUnexpectedAddressFormat)
	}

	port, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		_ = listener.Close()

		return nil, fmt.Errorf("parse listener port: %w", err)
	}

	return &localListener{Listener: listener, Port: port}, nil
}
