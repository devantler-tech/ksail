package mirror_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCaptureCommand_BuildsTcpdumpInvocation(t *testing.T) {
	t.Parallel()

	command, err := mirror.CaptureCommand(8080)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"tcpdump", "-p", "-i", "any", "-U", "-w", "-",
		"tcp", "port", "8080",
	}, command)
}

func TestCaptureCommand_AcceptsPortBounds(t *testing.T) {
	t.Parallel()

	for _, port := range []int{1, 65535} {
		command, err := mirror.CaptureCommand(port)

		require.NoError(t, err)
		assert.NotEmpty(t, command)
	}
}

func TestCaptureCommand_RejectsInvalidPorts(t *testing.T) {
	t.Parallel()

	for _, port := range []int{0, -1, 65536} {
		command, err := mirror.CaptureCommand(port)

		require.ErrorIs(t, err, mirror.ErrInvalidCapturePort)
		assert.Nil(t, command)
	}
}
