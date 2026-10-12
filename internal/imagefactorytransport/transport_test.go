package imagefactorytransport

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type recordedConn struct {
	net.Conn
	writes   [][]byte
	short    bool
	writeErr error
}

func (conn *recordedConn) Write(payload []byte) (int, error) {
	conn.writes = append(conn.writes, bytes.Clone(payload))
	if conn.writeErr != nil {
		return len(payload) / 2, conn.writeErr
	}
	if conn.short {
		return 0, nil
	}
	return len(payload), nil
}

func TestWriteSizingRejectsShortWritesAndPreservesErrors(t *testing.T) {
	t.Parallel()
	written, err := (writeSizedConn{Conn: &recordedConn{short: true}}).Write([]byte("synthetic"))
	require.Zero(t, written)
	require.ErrorIs(t, err, io.ErrShortWrite)
	written, err = (writeSizedConn{Conn: &recordedConn{writeErr: net.ErrClosed}}).Write([]byte("synthetic"))
	require.Equal(t, 4, written)
	require.ErrorIs(t, err, net.ErrClosed)
}

func TestNewSizesWritesAndPreservesDialContextAndTLS(t *testing.T) {
	t.Parallel()
	conn := &recordedConn{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	base := &http.Transport{
		ForceAttemptHTTP2: true,
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS13},
		DialContext: func(got context.Context, network, address string) (net.Conn, error) {
			require.Equal(t, ctx, got)
			require.Equal(t, "tcp", network)
			require.Equal(t, "synthetic.invalid:443", address)
			return conn, nil
		},
	}
	transport := New(base)
	defer transport.CloseIdleConnections()
	require.NotSame(t, base, transport)
	require.Equal(t, base.TLSClientConfig, transport.TLSClientConfig)
	require.False(t, transport.TLSClientConfig.InsecureSkipVerify)
	require.True(t, transport.ForceAttemptHTTP2)
	wrapped, err := transport.DialContext(ctx, "tcp", "synthetic.invalid:443")
	require.NoError(t, err)
	payload := bytes.Repeat([]byte("synthetic"), 400)
	written, err := wrapped.Write(payload)
	require.NoError(t, err)
	require.Equal(t, len(payload), written)
	require.Equal(t, payload, bytes.Join(conn.writes, nil))
	for _, chunk := range conn.writes {
		require.LessOrEqual(t, len(chunk), 1024)
	}
}

func TestNewPreservesDialErrors(t *testing.T) {
	t.Parallel()
	transport := New(&http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, net.ErrClosed
	}})
	defer transport.CloseIdleConnections()
	conn, err := transport.DialContext(t.Context(), "tcp", "synthetic.invalid:443")
	require.Nil(t, conn)
	require.True(t, errors.Is(err, net.ErrClosed))
}
