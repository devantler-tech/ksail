package steeragent

import (
	"context"
	"io"
	"net"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
)

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
		options{servicePort: servicePort, interceptPort: interceptPort},
		deps{transport: transport, listen: listen, runner: runner},
	)
}
