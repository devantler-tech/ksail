package kubernetes

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
)

// tcpProxy is a local TCP proxy that forwards connections to a Kubernetes pod
// through the SPDY port-forward protocol. Unlike the standard portforward library,
// it owns the TCP listener and re-dials the SPDY connection when it drops,
// making it resilient to transient SPDY tunnel failures.
//
// Each accepted TCP connection creates a pair of SPDY streams (data + error)
// on the current SPDY connection. If stream creation fails (dead connection),
// the proxy transparently re-dials before retrying.
type tcpProxy struct {
	listener   net.Listener
	localPort  int
	remotePort int
	dialer     httpstream.Dialer
	stopCh     chan struct{}

	mu        sync.Mutex
	spdyConn  httpstream.Connection
	requestID int
}

// newTCPProxy creates a TCP proxy listening on a random local port that forwards
// to the given remote pod port via the SPDY dialer.
func newTCPProxy(dialer httpstream.Dialer, remotePort int) (*tcpProxy, error) {
	ll, err := newLocalListener()
	if err != nil {
		return nil, fmt.Errorf("tcp proxy: %w", err)
	}

	return &tcpProxy{
		listener:   ll.Listener,
		localPort:  ll.Port,
		remotePort: remotePort,
		dialer:     dialer,
		stopCh:     make(chan struct{}),
	}, nil
}

// run accepts TCP connections and forwards each to the pod.
// It blocks until the proxy is stopped via close.
func (tp *tcpProxy) run() {
	for {
		conn, err := tp.listener.Accept()
		if err != nil {
			select {
			case <-tp.stopCh:
				return
			default:
				continue
			}
		}

		go tp.handleConnection(conn)
	}
}

// close stops the proxy and releases all resources.
func (tp *tcpProxy) close() {
	select {
	case <-tp.stopCh:
		return
	default:
		close(tp.stopCh)
	}

	tp.listener.Close()

	tp.mu.Lock()
	defer tp.mu.Unlock()

	if tp.spdyConn != nil {
		tp.spdyConn.Close()
		tp.spdyConn = nil
	}
}

// getConnection returns the current SPDY connection, re-dialing if it's dead or absent.
func (tp *tcpProxy) getConnection() (httpstream.Connection, error) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if tp.spdyConn != nil {
		select {
		case <-tp.spdyConn.CloseChan():
			// Connection died, close it and redial
			tp.spdyConn.Close()
			tp.spdyConn = nil
		default:
			return tp.spdyConn, nil
		}
	}

	conn, _, err := tp.dialer.Dial("portforward.k8s.io")
	if err != nil {
		return nil, fmt.Errorf("dial SPDY connection: %w", err)
	}

	tp.spdyConn = conn

	return conn, nil
}

// nextRequestID returns a unique request ID for stream creation.
func (tp *tcpProxy) nextRequestID() int {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	tp.requestID++

	return tp.requestID
}

// handleConnection forwards a single TCP connection to the pod via SPDY streams.
// If the SPDY connection is dead, it transparently re-dials before creating streams.
func (tp *tcpProxy) handleConnection(localConn net.Conn) {
	defer localConn.Close()

	conn, err := tp.getConnection()
	if err != nil {
		return
	}

	reqID := tp.nextRequestID()

	errorStream, dataStream, err := tp.createStreams(conn, reqID)
	if err != nil {
		// SPDY connection might have died between getConnection and createStreams.
		// Invalidate and retry once.
		tp.mu.Lock()
		if tp.spdyConn == conn {
			tp.spdyConn.Close()
			tp.spdyConn = nil
		}
		tp.mu.Unlock()

		conn, err = tp.getConnection()
		if err != nil {
			return
		}

		errorStream, dataStream, err = tp.createStreams(conn, reqID)
		if err != nil {
			return
		}
	}

	defer conn.RemoveStreams(errorStream, dataStream)

	// Read error stream in the background
	errorCh := make(chan error, 1)

	go func() {
		message, readErr := io.ReadAll(errorStream)

		switch {
		case readErr != nil:
			errorCh <- fmt.Errorf("error stream read: %w", readErr)
		case len(message) > 0:
			errorCh <- fmt.Errorf("port-forward error: %s", string(message))
		}

		close(errorCh)
	}()

	// Bidirectional data relay.
	//
	// Now that the exec tunnel handles Docker API connections (which use HTTP
	// hijacking), this proxy is only used for K8s API (regular HTTPS). We call
	// dataStream.Close() when local→remote sees EOF to signal the remote that
	// the client is done writing, which allows HTTP keep-alive connections to
	// terminate cleanly.
	localDone := make(chan struct{})
	remoteDone := make(chan struct{})

	go func() {
		_, _ = io.Copy(localConn, dataStream)
		close(remoteDone)
	}()

	go func() {
		_, _ = io.Copy(dataStream, localConn)
		_ = dataStream.Close()
		close(localDone)
	}()

	select {
	case <-remoteDone:
	case <-localDone:
	}

	// Reset data stream to discard unsent data and unblock the error stream.
	_ = dataStream.Reset()

	// Always drain error channel to avoid goroutine leak.
	<-errorCh
}

// createStreams creates the SPDY error and data stream pair for a single forwarded connection.
func (tp *tcpProxy) createStreams(
	conn httpstream.Connection,
	requestID int,
) (errorStream, dataStream httpstream.Stream, err error) {
	headers := http.Header{}
	headers.Set(v1.StreamType, v1.StreamTypeError)
	headers.Set(v1.PortHeader, strconv.Itoa(tp.remotePort))
	headers.Set(v1.PortForwardRequestIDHeader, strconv.Itoa(requestID))

	errorStream, err = conn.CreateStream(headers)
	if err != nil {
		return nil, nil, fmt.Errorf("create error stream: %w", err)
	}

	headers.Set(v1.StreamType, v1.StreamTypeData)

	dataStream, err = conn.CreateStream(headers)
	if err != nil {
		errorStream.Close()

		return nil, nil, fmt.Errorf("create data stream: %w", err)
	}

	return errorStream, dataStream, nil
}
