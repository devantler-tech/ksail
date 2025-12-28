package parallel_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/cmd/parallel"
)

// BenchmarkExecutor_Sequential benchmarks sequential execution of tasks.
func BenchmarkExecutor_Sequential(b *testing.B) {
	executor := parallel.NewExecutor(1) // Force sequential
	tasks := createBenchmarkTasks(10, 1*time.Millisecond)

	b.ResetTimer()

	for b.Loop() {
		_ = executor.Execute(context.Background(), tasks...)
	}
}

// BenchmarkExecutor_Parallel benchmarks parallel execution of tasks.
func BenchmarkExecutor_Parallel(b *testing.B) {
	executor := parallel.NewExecutor(4)
	tasks := createBenchmarkTasks(10, 1*time.Millisecond)

	b.ResetTimer()

	for b.Loop() {
		_ = executor.Execute(context.Background(), tasks...)
	}
}

// BenchmarkExecutor_HighConcurrency benchmarks with maximum concurrency.
func BenchmarkExecutor_HighConcurrency(b *testing.B) {
	executor := parallel.NewExecutor(8)
	tasks := createBenchmarkTasks(20, 1*time.Millisecond)

	b.ResetTimer()

	for b.Loop() {
		_ = executor.Execute(context.Background(), tasks...)
	}
}

// createBenchmarkTasks creates a slice of tasks that simulate I/O work.
func createBenchmarkTasks(numTasks int, delay time.Duration) []parallel.Task {
	tasks := make([]parallel.Task, numTasks)
	for taskIdx := range tasks {
		tasks[taskIdx] = func(_ context.Context) error {
			time.Sleep(delay)

			return nil
		}
	}

	return tasks
}
