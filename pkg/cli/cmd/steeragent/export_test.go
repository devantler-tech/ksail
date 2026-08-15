package steeragent

import (
	"context"
	"io"
	"net"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
)

// NewStdioTransportForTest exposes the production stdio transport with
// injected pipe ends so the black-box test package can prove Close interrupts
// blocked reads and writes.
func NewStdioTransportForTest(in, out *os.File) io.ReadWriteCloser {
	return stdioTransport{in: in, out: out}
}

// ListenInterceptForTest exposes the production listener factory so the
// black-box test package can pin its bind address.
func ListenInterceptForTest(ctx context.Context, port int) (net.Listener, error) {
	return listenIntercept(ctx, port)
}

// RunForTest exposes the internal run composition to the black-box test package
// with its port options and seams injected.
func RunForTest(
	ctx context.Context,
	servicePort, interceptPort int,
	transport io.ReadWriteCloser,
	listen func(context.Context, int) (net.Listener, error),
	runner mirror.SteerCommandRunner,
) error {
	return run(
		ctx,
		options{servicePort: servicePort, interceptPort: interceptPort, expectKeepalives: false},
		deps{transport: transport, listen: listen, runner: runner},
	)
}
