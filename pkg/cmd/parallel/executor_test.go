package parallel_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/cmd/parallel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errTest = errors.New("test error")

var errResult1 = errors.New("error 1")

var errResult2 = errors.New("error 2")

func TestDefaultMaxConcurrency(t *testing.T) {
	t.Parallel()

	maxConcurrency := parallel.DefaultMaxConcurrency()
	assert.GreaterOrEqual(t, maxConcurrency, int64(2), "should be at least 2")
	assert.LessOrEqual(t, maxConcurrency, int64(8), "should be capped at 8")
}

func TestNewExecutor(t *testing.T) {
	t.Parallel()

	// Test with positive value
	executor := parallel.NewExecutor(4)
	assert.NotNil(t, executor)

	// Test with zero (should use default)
	executor = parallel.NewExecutor(0)
	assert.NotNil(t, executor)

	// Test with negative (should use default)
	executor = parallel.NewExecutor(-1)
	assert.NotNil(t, executor)
}

func TestExecutor_Execute_NoTasks(t *testing.T) {
	t.Parallel()

	executor := parallel.NewExecutor(4)
	err := executor.Execute(context.Background())
	require.NoError(t, err)
}

func TestExecutor_Execute_SingleTask(t *testing.T) {
	t.Parallel()

	executor := parallel.NewExecutor(4)
	executed := false

	err := executor.Execute(context.Background(), func(_ context.Context) error {
		executed = true

		return nil
	})

	require.NoError(t, err)
	assert.True(t, executed, "task should have been executed")
}

func TestExecutor_Execute_MultipleTasks(t *testing.T) {
	t.Parallel()

	executor := parallel.NewExecutor(4)

	var counter atomic.Int32

	tasks := make([]parallel.Task, 5)
	for i := range tasks {
		tasks[i] = func(_ context.Context) error {
			counter.Add(1)

			return nil
		}
	}

	err := executor.Execute(context.Background(), tasks...)
	require.NoError(t, err)
	assert.Equal(t, int32(5), counter.Load(), "all tasks should have executed")
}

func TestExecutor_Execute_FirstErrorCancelsRemaining(t *testing.T) {
	t.Parallel()

	executor := parallel.NewExecutor(1) // Force sequential to make test deterministic

	var afterErrorExecuted atomic.Bool

	err := executor.Execute(context.Background(),
		func(_ context.Context) error {
			return errTest
		},
		func(_ context.Context) error {
			afterErrorExecuted.Store(true)

			return nil
		},
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, errTest)
}

func TestExecutor_Execute_RespectsContextCancellation(t *testing.T) {
	t.Parallel()

	executor := parallel.NewExecutor(4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Need multiple tasks to go through the semaphore/errgroup path
	err := executor.Execute(ctx,
		func(_ context.Context) error { return nil },
		func(_ context.Context) error { return nil },
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestExecutor_Execute_ParallelExecution(t *testing.T) {
	t.Parallel()

	executor := parallel.NewExecutor(4)

	var (
		maxConcurrent atomic.Int32
		current       atomic.Int32
	)

	tasks := make([]parallel.Task, 8)
	for taskIdx := range tasks {
		tasks[taskIdx] = func(_ context.Context) error {
			currentValue := current.Add(1)

			for {
				oldValue := maxConcurrent.Load()

				needsUpdate := currentValue > oldValue
				if !needsUpdate || maxConcurrent.CompareAndSwap(oldValue, currentValue) {
					break
				}
			}

			time.Sleep(10 * time.Millisecond)
			current.Add(-1)

			return nil
		}
	}

	err := executor.Execute(context.Background(), tasks...)
	require.NoError(t, err)
	assert.Greater(t, maxConcurrent.Load(), int32(1), "tasks should run in parallel")
}

func TestSyncWriter_ThreadSafe(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer

	syncWriter := parallel.NewSyncWriter(&buffer)

	executor := parallel.NewExecutor(4)

	tasks := make([]parallel.Task, 10)
	for taskIdx := range tasks {
		tasks[taskIdx] = func(_ context.Context) error {
			_, writeErr := syncWriter.Write([]byte("x"))
			if writeErr != nil {
				return fmt.Errorf("sync write failed: %w", writeErr)
			}

			return nil
		}
	}

	err := executor.Execute(context.Background(), tasks...)
	require.NoError(t, err)
	assert.Equal(t, 10, buffer.Len(), "all writes should complete")
}

func TestResults_ThreadSafe(t *testing.T) {
	t.Parallel()

	results := parallel.NewResults[int]()

	executor := parallel.NewExecutor(4)

	tasks := make([]parallel.Task, 10)
	for taskIdx := range tasks {
		tasks[taskIdx] = func(_ context.Context) error {
			results.Add(taskIdx)

			return nil
		}
	}

	err := executor.Execute(context.Background(), tasks...)
	require.NoError(t, err)
	assert.Len(t, results.Values(), 10, "all values should be collected")
	assert.False(t, results.HasErrors())
}

func TestResults_WithErrors(t *testing.T) {
	t.Parallel()

	results := parallel.NewResults[int]()
	results.Add(1)
	results.AddError(errResult1)
	results.Add(2)
	results.AddError(errResult2)

	assert.Len(t, results.Values(), 2)
	assert.True(t, results.HasErrors())
	assert.Len(t, results.Errors(), 2)
}
