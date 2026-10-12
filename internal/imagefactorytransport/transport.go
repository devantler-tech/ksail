// Package imagefactorytransport sizes writes without changing TLS security defaults.
package imagefactorytransport

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const dialBudget = 30 * time.Second

// Client preserves a custom default transport, or clones the standard transport.
func Client() http.Client {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.Client{Transport: http.DefaultTransport}
	}
	return http.Client{Transport: New(base)}
}

// New gives registration its own transport while preserving the caller's settings.
func New(base *http.Transport) *http.Transport {
	transport := base.Clone()
	dial := transport.DialContext
	if dial == nil {
		dialer := &net.Dialer{Timeout: dialBudget, KeepAlive: dialBudget}
		dial = dialer.DialContext
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return writeSizedConn{Conn: conn}, nil
	}
	return transport
}

// writeSizedConn preserves the complete byte stream, deadlines and connection lifecycle.
// Small writes avoid the hosted-runner stall reproduced with larger TLS ClientHello writes;
// the TLS record, protocol versions, hybrid groups and certificate checks are unchanged.
type writeSizedConn struct{ net.Conn }

func (conn writeSizedConn) Write(payload []byte) (int, error) {
	const maximumWrite = 1024
	written := 0
	for len(payload) > 0 {
		size := min(len(payload), maximumWrite)
		n, err := conn.Conn.Write(payload[:size])
		written += n
		if err != nil {
			return written, fmt.Errorf("write Image Factory connection: %w", err)
		}
		if n != size {
			return written, io.ErrShortWrite
		}
		payload = payload[size:]
	}
	return written, nil
}
