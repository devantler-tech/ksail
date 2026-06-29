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

// FlowObserver retrieves flows from a Hubble data source. It is the seam the
// command depends on, so tests can supply canned flows without a relay.
type FlowObserver interface {
	// ObserveFlows returns up to last recent flows, most-recent last.
	ObserveFlows(ctx context.Context, last uint64) ([]FlowRecord, error)
	// StreamFlows emits flows as they arrive — the last backlog flows first,
	// then live flows — invoking emit for each until ctx is cancelled or the
	// source closes the stream. It is the follow-mode counterpart to
	// ObserveFlows, so the command never has to buffer an unbounded stream.
	StreamFlows(ctx context.Context, last uint64, emit func(FlowRecord) error) error
}

// Options controls a single [Observe] or [Stream] invocation.
type Options struct {
	Last   uint64
	Filter FilterOptions
	Output string
	// Follow selects continuous streaming ([Stream]) over a one-shot query
	// ([Observe]).
	Follow bool
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

// Stream observes flows continuously from observer, applies the option filters,
// and renders each surviving flow to out as it arrives, until ctx is cancelled
// or the source closes the stream. Plain output prints the table header once up
// front then one row per flow; JSON output is newline-delimited (one flow
// object per line) so it stays valid for an unbounded stream. The output format
// is validated before streaming so an invalid format fails fast.
func Stream(ctx context.Context, observer FlowObserver, opts Options, out io.Writer) error {
	err := validateOutput(opts.Output)
	if err != nil {
		return err
	}

	if opts.Output != OutputJSON {
		err = FormatPlainHeader(out)
		if err != nil {
			return err
		}
	}

	err = observer.StreamFlows(ctx, opts.Last, streamEmitter(opts, out))
	if err != nil {
		return fmt.Errorf("stream flows: %w", err)
	}

	return nil
}

// streamEmitter returns an emit callback that drops flows failing the filter
// and renders each surviving flow to out in the requested streaming format.
func streamEmitter(opts Options, out io.Writer) func(FlowRecord) error {
	return func(record FlowRecord) error {
		if !record.matches(opts.Filter) {
			return nil
		}

		if opts.Output == OutputJSON {
			return FormatJSONLine(out, record)
		}

		return FormatPlainLine(out, record)
	}
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

// flowStream is the receive side of a Hubble GetFlows stream. Narrowing the
// generated client to just Recv keeps receiveFlows testable and the two relay
// paths sharing one receive loop.
type flowStream interface {
	Recv() (*observerpb.GetFlowsResponse, error)
}

// ObserveFlows dials the relay, requests the last recent flows, and projects
// each into a [FlowRecord]. The connection is closed before returning.
func (o *RelayObserver) ObserveFlows(ctx context.Context, last uint64) ([]FlowRecord, error) {
	conn, stream, err := o.getFlows(ctx, &observerpb.GetFlowsRequest{Number: last})
	if err != nil {
		return nil, err
	}

	defer func() { _ = conn.Close() }()

	var records []FlowRecord

	err = receiveFlows(stream, o.address, func(record FlowRecord) error {
		records = append(records, record)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return records, nil
}

// StreamFlows dials the relay in follow mode and emits each flow as it arrives
// (the last backlog flows first, then live flows) until ctx is cancelled or the
// relay closes the stream. A cancellation is a clean stop, not an error. The
// connection is closed before returning.
func (o *RelayObserver) StreamFlows(
	ctx context.Context,
	last uint64,
	emit func(FlowRecord) error,
) error {
	conn, stream, err := o.getFlows(ctx, &observerpb.GetFlowsRequest{Number: last, Follow: true})
	if err != nil {
		return err
	}

	defer func() { _ = conn.Close() }()

	err = receiveFlows(stream, o.address, emit)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}

		return err
	}

	return nil
}

// getFlows opens a GetFlows stream for req, returning the connection (the
// caller must Close it) alongside the stream.
func (o *RelayObserver) getFlows(
	ctx context.Context,
	req *observerpb.GetFlowsRequest,
) (*grpc.ClientConn, flowStream, error) {
	conn, err := grpc.NewClient(o.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("connect to hubble relay %q: %w", o.address, err)
	}

	stream, err := observerpb.NewObserverClient(conn).GetFlows(ctx, req)
	if err != nil {
		_ = conn.Close()

		return nil, nil, fmt.Errorf("request flows from hubble relay %q: %w", o.address, err)
	}

	return conn, stream, nil
}

// receiveFlows pulls responses from stream until it ends (io.EOF), projecting
// each flow into a [FlowRecord] and passing it to emit. A receive error is
// wrapped with the relay address; an emit error stops the loop and propagates
// unchanged so callers can match it.
func receiveFlows(stream flowStream, address string, emit func(FlowRecord) error) error {
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("receive flow from hubble relay %q: %w", address, err)
		}

		if observed := response.GetFlow(); observed != nil {
			emitErr := emit(recordFromFlow(observed))
			if emitErr != nil {
				return emitErr
			}
		}
	}
}
