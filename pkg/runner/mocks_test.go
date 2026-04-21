package runner_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/runner"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errMockFailed = errors.New("mock error")

func TestMockCommandRunner_ReturnsStaticResult(t *testing.T) {
	t.Parallel()

	mock := runner.NewMockCommandRunner(t)

	expected := runner.CommandResult{Stdout: "hello", Stderr: ""}

	mock.EXPECT().
		Run(context.Background(), (*cobra.Command)(nil), []string{"arg1"}).
		Return(expected, nil)

	result, err := mock.Run(context.Background(), nil, []string{"arg1"})

	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestMockCommandRunner_ReturnsError(t *testing.T) {
	t.Parallel()

	mock := runner.NewMockCommandRunner(t)

	expected := runner.CommandResult{Stdout: "partial", Stderr: "oops"}

	mock.EXPECT().
		Run(context.Background(), (*cobra.Command)(nil), []string(nil)).
		Return(expected, errMockFailed)

	result, err := mock.Run(context.Background(), nil, nil)

	require.ErrorIs(t, err, errMockFailed)
	assert.Equal(t, expected, result)
}

func TestMockCommandRunner_RunCallback(t *testing.T) {
	t.Parallel()

	mock := runner.NewMockCommandRunner(t)

	var calledArgs []string

	mock.EXPECT().
		Run(context.Background(), (*cobra.Command)(nil), []string{"a", "b"}).
		Run(func(_ context.Context, _ *cobra.Command, args []string) {
			calledArgs = args
		}).
		Return(runner.CommandResult{Stdout: "done"}, nil)

	result, err := mock.Run(context.Background(), nil, []string{"a", "b"})

	require.NoError(t, err)
	assert.Equal(t, "done", result.Stdout)
	assert.Equal(t, []string{"a", "b"}, calledArgs)
}

func TestMockCommandRunner_RunAndReturn(t *testing.T) {
	t.Parallel()

	mock := runner.NewMockCommandRunner(t)

	mock.EXPECT().
		Run(context.Background(), (*cobra.Command)(nil), []string{"x"}).
		RunAndReturn(func(_ context.Context, _ *cobra.Command, args []string) (runner.CommandResult, error) {
			return runner.CommandResult{
				Stdout: "processed " + args[0],
			}, nil
		})

	result, err := mock.Run(context.Background(), nil, []string{"x"})

	require.NoError(t, err)
	assert.Equal(t, "processed x", result.Stdout)
}

func TestMockCommandRunner_MultipleExpectations(t *testing.T) {
	t.Parallel()

	mock := runner.NewMockCommandRunner(t)

	cmd := &cobra.Command{}

	mock.EXPECT().
		Run(context.Background(), cmd, []string{"first"}).
		Return(runner.CommandResult{Stdout: "result-1"}, nil).Once()

	mock.EXPECT().
		Run(context.Background(), cmd, []string{"second"}).
		Return(runner.CommandResult{Stdout: "result-2"}, nil).Once()

	r1, err := mock.Run(context.Background(), cmd, []string{"first"})

	require.NoError(t, err)
	assert.Equal(t, "result-1", r1.Stdout)

	r2, err := mock.Run(context.Background(), cmd, []string{"second"})

	require.NoError(t, err)
	assert.Equal(t, "result-2", r2.Stdout)
}
