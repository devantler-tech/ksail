package hubble

import (
	"context"
	"errors"
	"fmt"
	"io"

	observerpb "github.com/cilium/cilium/api/v1/observer"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Output formats accepted by [Observe].
const (
	OutputPlain = "plain"
	OutputJSON  = "json"
)

// DefaultRelayAddress is the address `hubble observe` uses by default; the user
// is expected to port-forward the in-cluster Hubble Relay service to it
// (for example `kubectl -n kube-system port-forward svc/hubble-relay 4245:80`).
const DefaultRelayAddress = "localhost:4245"

// ErrUnknownOutputFormat is returned for an unrecognized output format.
var ErrUnknownOutputFormat = errors.New("unknown output format")

// FlowObserver retrieves recent flows from a Hubble data source. It is the seam
// the command depends on, so tests can supply canned flows without a relay.
type FlowObserver interface {
	// ObserveFlows returns up to last recent flows, most-recent last.
	ObserveFlows(ctx context.Context, last uint64) ([]FlowRecord, error)
}

// Options controls a single [Observe] invocation.
type Options struct {
	Last   uint64
	Filter FilterOptions
	Output string
}

// Observe queries flows from observer, applies the option filters, and renders
// the result to out in the requested format. The output format is validated
// before any flows are fetched, so an invalid format fails fast without a
// (possibly slow or failing) relay round-trip masking the real input error.
func Observe(ctx context.Context, observer FlowObserver, opts Options, out io.Writer) error {
	err := validateOutput(opts.Output)
	if err != nil {
		return err
	}

	records, err := observer.ObserveFlows(ctx, opts.Last)
	if err != nil {
		return fmt.Errorf("observe flows: %w", err)
	}

	records = FilterFlows(records, opts.Filter)

	if opts.Output == OutputJSON {
		return FormatJSON(out, records)
	}

	return FormatPlain(out, records)
}

// validateOutput rejects an unrecognized output format.
func validateOutput(output string) error {
	switch output {
	case OutputJSON, OutputPlain, "":
		return nil
	default:
		return fmt.Errorf(
			"%w: %s (valid: %s, %s)",
			ErrUnknownOutputFormat, output, OutputPlain, OutputJSON,
		)
	}
}

// RelayObserver observes flows from a Hubble Relay gRPC endpoint.
type RelayObserver struct {
	address string
}

// NewRelayObserver returns a [RelayObserver] for the given relay address.
func NewRelayObserver(address string) *RelayObserver {
	return &RelayObserver{address: address}
}

// ObserveFlows dials the relay, requests the last recent flows, and projects
// each into a [FlowRecord]. The connection is closed before returning.
func (o *RelayObserver) ObserveFlows(ctx context.Context, last uint64) ([]FlowRecord, error) {
	conn, err := grpc.NewClient(o.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect to hubble relay %q: %w", o.address, err)
	}

	defer func() { _ = conn.Close() }()

	stream, err := observerpb.NewObserverClient(conn).GetFlows(ctx, &observerpb.GetFlowsRequest{
		Number: last,
	})
	if err != nil {
		return nil, fmt.Errorf("request flows from hubble relay %q: %w", o.address, err)
	}

	var records []FlowRecord

	for {
		response, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}

		if recvErr != nil {
			return nil, fmt.Errorf("receive flow from hubble relay %q: %w", o.address, recvErr)
		}

		if observed := response.GetFlow(); observed != nil {
			records = append(records, recordFromFlow(observed))
		}
	}

	return records, nil
}
