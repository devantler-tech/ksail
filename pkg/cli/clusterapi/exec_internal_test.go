package clusterapi

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResizeQueueNext(t *testing.T) {
	t.Parallel()

	channel := make(chan api.TerminalSize, 1)
	queue := resizeQueue{resize: channel}

	channel <- api.TerminalSize{Rows: 24, Cols: 80}

	size := queue.Next()
	require.NotNil(t, size)
	assert.Equal(t, uint16(80), size.Width)
	assert.Equal(t, uint16(24), size.Height)

	// A closed channel ends the queue (nil), which stops remotecommand resizing.
	close(channel)
	assert.Nil(t, queue.Next())
}
