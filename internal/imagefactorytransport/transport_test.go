package imagefactorytransport_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/imagefactorytransport"
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
	for _, testCase := range []struct {
		name        string
		conn        *recordedConn
		wantWritten int
		wantErr     error
	}{
		{"short", &recordedConn{short: true}, 0, io.ErrShortWrite},
		{"failed", &recordedConn{writeErr: net.ErrClosed}, 4, net.ErrClosed},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			transport := imagefactorytransport.New(&http.Transport{
				DialContext: func(context.Context, string, string) (net.Conn, error) { return testCase.conn, nil },
			})
			defer transport.CloseIdleConnections()
			conn, err := transport.DialContext(t.Context(), "tcp", "synthetic.invalid:443")
			require.NoError(t, err)
			written, err := conn.Write([]byte("synthetic"))
			require.Equal(t, testCase.wantWritten, written)
			require.ErrorIs(t, err, testCase.wantErr)
		})
	}
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
	transport := imagefactorytransport.New(base)
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
	base := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) { return nil, net.ErrClosed },
	}
	transport := imagefactorytransport.New(base)
	defer transport.CloseIdleConnections()
	conn, err := transport.DialContext(t.Context(), "tcp", "synthetic.invalid:443")
	require.Nil(t, conn)
	require.ErrorIs(t, err, net.ErrClosed)
}

func TestClientOwnsItsStandardTransport(t *testing.T) {
	t.Parallel()
	client := imagefactorytransport.Client()
	defer client.CloseIdleConnections()
	require.NotNil(t, client.Transport)
	require.NotSame(t, http.DefaultTransport, client.Transport)
	base, ok := http.DefaultTransport.(*http.Transport)
	require.True(t, ok)
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.Equal(t, base.TLSClientConfig, transport.TLSClientConfig)
}

func TestMissingDialFunctionPreservesCancellation(t *testing.T) {
	t.Parallel()
	transport := imagefactorytransport.New(&http.Transport{})
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	conn, err := transport.DialContext(ctx, "tcp", "127.0.0.1:1")
	require.Nil(t, conn)
	require.ErrorIs(t, err, context.Canceled)
}
