package helpers_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/stretchr/testify/assert"
)

func TestNewStandardIOStreams(t *testing.T) {
	t.Parallel()

	streams := helpers.NewStandardIOStreams()

	// Verify all three streams are non-nil
	assert.NotNil(t, streams.In)
	assert.NotNil(t, streams.Out)
	assert.NotNil(t, streams.ErrOut)
}

func TestNewStandardIOStreams_StreamsAreWritable(t *testing.T) {
	t.Parallel()

	// This verifies that the streams from NewStandardIOStreams can be used
	// We don't want to actually write to stdout/stderr in tests,
	// so we just verify the interface types work

	streams := helpers.NewStandardIOStreams()

	// Verify streams implement the expected interfaces
	var outBuf bytes.Buffer

	_, err := outBuf.WriteString("test")
	assert.NoError(t, err)

	// streams.Out is io.Writer
	assert.Implements(t, (*interface{ Write([]byte) (int, error) })(nil), streams.Out)

	// streams.ErrOut is io.Writer
	assert.Implements(t, (*interface{ Write([]byte) (int, error) })(nil), streams.ErrOut)

	// streams.In is io.Reader
	assert.Implements(t, (*interface{ Read([]byte) (int, error) })(nil), streams.In)
}
