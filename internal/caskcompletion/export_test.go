package caskcompletion

import (
	"context"
	"io"
)

// DecodeLineForTest exposes the actual event decoder.
var DecodeLineForTest = decodeLine //nolint:gochecknoglobals

// ProbeForTest changes only the controlled input and fixture brew, never completion registration.
func ProbeForTest(ctx context.Context, brew, input, root string) (Result, error) {
	return probe(ctx, brew, input, root)
}

// ReadBufferForTest observes the real streaming decoder without starting a terminal.
func ReadBufferForTest(ctx context.Context, output io.Reader, nonce string) (string, error) {
	events := make(chan observation, eventCapacity)
	go scanEvents(ctx, output, nonce, events)

	return awaitEvent(ctx, events, "BUFFER")
}
