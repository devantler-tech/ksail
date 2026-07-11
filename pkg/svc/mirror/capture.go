package mirror

import (
	"errors"
	"fmt"
	"strconv"
)

// maxPort is the highest valid TCP port number.
const maxPort = 65535

// ErrInvalidCapturePort is returned by CaptureCommand for a port outside
// 1–65535.
var ErrInvalidCapturePort = errors.New("capture port must be between 1 and 65535")

// CaptureCommand returns the tcpdump invocation the mirror execs inside the
// tap container to produce the read-only capture stream: pcap for TCP traffic
// on the given service port, written packet-buffered ("-U") to stdout ("-w -")
// so the exec channel carries it to the local process with no intermediate
// file (the tap's root filesystem is read-only). "-p" keeps the interface out
// of promiscuous mode — mirror mode only observes the pod's own traffic — and
// "-i any" covers every pod interface. Capture needs CAP_NET_RAW, which is the
// one capability the injected tap retains (see InjectTap).
func CaptureCommand(port int) ([]string, error) {
	if port < 1 || port > maxPort {
		return nil, fmt.Errorf("%w: %d", ErrInvalidCapturePort, port)
	}

	return []string{
		"tcpdump", "-p", "-i", "any", "-U", "-w", "-",
		protocolTCP, "port", strconv.Itoa(port),
	}, nil
}
