package parallel

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"sync"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

const (
	// minConcurrency is the minimum number of concurrent tasks.
	minConcurrency = 2
	// maxConcurrencyCap caps concurrency to avoid overwhelming Docker/Kubernetes APIs.
	maxConcurrencyCap = 8
)

// DefaultMaxConcurrency returns the default maximum concurrency based on available CPUs.
func DefaultMaxConcurrency() int64 {
	numCPU := int64(runtime.NumCPU())

	return min(max(numCPU, minConcurrency), maxConcurrencyCap)
}

// Executor provides controlled parallel execution of tasks.
type Executor struct {
	maxConcurrency int64
}

// NewExecutor creates a new parallel executor with the specified max concurrency.
// If maxConcurrency <= 0, DefaultMaxConcurrency() is used.
func NewExecutor(maxConcurrency int64) *Executor {
	if maxConcurrency <= 0 {
		maxConcurrency = DefaultMaxConcurrency()
	}

	return &Executor{maxConcurrency: maxConcurrency}
}

// Task represents a unit of work that can be executed in parallel.
type Task func(ctx context.Context) error

// Execute runs all tasks concurrently with controlled parallelism.
// It returns the first error encountered, canceling remaining tasks.
// If all tasks succeed, it returns nil.
func (executor *Executor) Execute(ctx context.Context, tasks ...Task) error {
	if len(tasks) == 0 {
		return nil
	}

	// For a single task, just run it directly
	if len(tasks) == 1 {
		return tasks[0](ctx)
	}

	sem := semaphore.NewWeighted(executor.maxConcurrency)
	group, groupCtx := errgroup.WithContext(ctx)

	for _, task := range tasks {
		group.Go(func() error {
			acquireErr := sem.Acquire(groupCtx, 1)
			if acquireErr != nil {
				return fmt.Errorf("acquire semaphore: %w", acquireErr)
			}

			defer sem.Release(1)

			return task(groupCtx)
		})
	}

	waitErr := group.Wait()
	if waitErr != nil {
		return fmt.Errorf("parallel execution: %w", waitErr)
	}

	return nil
}

// SyncWriter is a thread-safe writer that serializes writes from multiple goroutines.
type SyncWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

// NewSyncWriter creates a new synchronized writer wrapping the given writer.
func NewSyncWriter(writer io.Writer) *SyncWriter {
	return &SyncWriter{writer: writer}
}

// Write writes data to the underlying writer with synchronization.
func (syncWriter *SyncWriter) Write(data []byte) (int, error) {
	syncWriter.mu.Lock()
	defer syncWriter.mu.Unlock()

	written, writeErr := syncWriter.writer.Write(data)
	if writeErr != nil {
		return written, fmt.Errorf("sync write: %w", writeErr)
	}

	return written, nil
}

// Results collects results from parallel tasks with thread-safe access.
type Results[T any] struct {
	mu     sync.Mutex
	values []T
	errors []error
}

// NewResults creates a new Results collector.
func NewResults[T any]() *Results[T] {
	return &Results[T]{}
}

// Add appends a result value.
func (results *Results[T]) Add(value T) {
	results.mu.Lock()
	defer results.mu.Unlock()

	results.values = append(results.values, value)
}

// AddError appends an error.
func (results *Results[T]) AddError(err error) {
	results.mu.Lock()
	defer results.mu.Unlock()

	results.errors = append(results.errors, err)
}

// Values returns all collected values.
func (results *Results[T]) Values() []T {
	results.mu.Lock()
	defer results.mu.Unlock()

	return results.values
}

// Errors returns all collected errors.
func (results *Results[T]) Errors() []error {
	results.mu.Lock()
	defer results.mu.Unlock()

	return results.errors
}

// HasErrors returns true if any errors were collected.
func (results *Results[T]) HasErrors() bool {
	results.mu.Lock()
	defer results.mu.Unlock()

	return len(results.errors) > 0
}
