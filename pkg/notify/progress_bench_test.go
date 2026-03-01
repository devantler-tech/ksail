package notify_test

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
)

// BenchmarkProgressGroup_Sequential benchmarks progress group with tasks running sequentially
// (simulates scenarios where tasks cannot be parallelized).
func BenchmarkProgressGroup_Sequential(b *testing.B) {
	// Use a no-op writer to avoid I/O overhead in benchmarks
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{Name: "task-1", Fn: func(ctx context.Context) error { time.Sleep(10 * time.Millisecond); return nil }},
		{Name: "task-2", Fn: func(ctx context.Context) error { time.Sleep(10 * time.Millisecond); return nil }},
		{Name: "task-3", Fn: func(ctx context.Context) error { time.Sleep(10 * time.Millisecond); return nil }},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)
		_ = pg.Run(context.Background(), tasks...)
	}
}

// BenchmarkProgressGroup_Parallel_Fast benchmarks parallel execution with fast tasks
// (simulates many quick operations like validation checks).
func BenchmarkProgressGroup_Parallel_Fast(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{Name: "task-1", Fn: func(ctx context.Context) error { time.Sleep(1 * time.Millisecond); return nil }},
		{Name: "task-2", Fn: func(ctx context.Context) error { time.Sleep(1 * time.Millisecond); return nil }},
		{Name: "task-3", Fn: func(ctx context.Context) error { time.Sleep(1 * time.Millisecond); return nil }},
		{Name: "task-4", Fn: func(ctx context.Context) error { time.Sleep(1 * time.Millisecond); return nil }},
		{Name: "task-5", Fn: func(ctx context.Context) error { time.Sleep(1 * time.Millisecond); return nil }},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)
		_ = pg.Run(context.Background(), tasks...)
	}
}

// BenchmarkProgressGroup_Parallel_Slow benchmarks parallel execution with slower tasks
// (simulates real-world scenarios like component installation).
func BenchmarkProgressGroup_Parallel_Slow(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{Name: "task-1", Fn: func(ctx context.Context) error { time.Sleep(50 * time.Millisecond); return nil }},
		{Name: "task-2", Fn: func(ctx context.Context) error { time.Sleep(50 * time.Millisecond); return nil }},
		{Name: "task-3", Fn: func(ctx context.Context) error { time.Sleep(50 * time.Millisecond); return nil }},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)
		_ = pg.Run(context.Background(), tasks...)
	}
}

// BenchmarkProgressGroup_ManyTasks benchmarks progress group with many parallel tasks
// (simulates high concurrency scenarios like batch operations).
func BenchmarkProgressGroup_ManyTasks(b *testing.B) {
	writer := io.Discard

	// Create 20 tasks
	tasks := make([]notify.ProgressTask, 20)
	for i := 0; i < 20; i++ {
		taskNum := i
		tasks[i] = notify.ProgressTask{
			Name: string(rune('a' + taskNum)),
			Fn: func(ctx context.Context) error {
				time.Sleep(5 * time.Millisecond)
				return nil
			},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)
		_ = pg.Run(context.Background(), tasks...)
	}
}

// BenchmarkProgressGroup_WithTimer benchmarks progress group with timer overhead.
func BenchmarkProgressGroup_WithTimer(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{Name: "task-1", Fn: func(ctx context.Context) error { time.Sleep(5 * time.Millisecond); return nil }},
		{Name: "task-2", Fn: func(ctx context.Context) error { time.Sleep(5 * time.Millisecond); return nil }},
		{Name: "task-3", Fn: func(ctx context.Context) error { time.Sleep(5 * time.Millisecond); return nil }},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tmr := timer.New()
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer, notify.WithTimer(tmr))
		_ = pg.Run(context.Background(), tasks...)
	}
}

// BenchmarkProgressGroup_CI_Mode benchmarks CI mode (no TTY, simpler output).
// This simulates the non-interactive output path used in CI/CD pipelines.
func BenchmarkProgressGroup_CI_Mode(b *testing.B) {
	// Use a buffer to simulate CI output (not a TTY)
	var buf bytes.Buffer

	tasks := []notify.ProgressTask{
		{Name: "task-1", Fn: func(ctx context.Context) error { time.Sleep(5 * time.Millisecond); return nil }},
		{Name: "task-2", Fn: func(ctx context.Context) error { time.Sleep(5 * time.Millisecond); return nil }},
		{Name: "task-3", Fn: func(ctx context.Context) error { time.Sleep(5 * time.Millisecond); return nil }},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		pg := notify.NewProgressGroup("Benchmarking", "⏱", &buf)
		_ = pg.Run(context.Background(), tasks...)
	}
}

// BenchmarkProgressGroup_CustomLabels benchmarks progress group with custom labels.
func BenchmarkProgressGroup_CustomLabels(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{Name: "task-1", Fn: func(ctx context.Context) error { time.Sleep(5 * time.Millisecond); return nil }},
		{Name: "task-2", Fn: func(ctx context.Context) error { time.Sleep(5 * time.Millisecond); return nil }},
	}

	labels := notify.InstallingLabels()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pg := notify.NewProgressGroup("Installing", "📦", writer, notify.WithLabels(labels))
		_ = pg.Run(context.Background(), tasks...)
	}
}

// BenchmarkProgressGroup_SingleTask benchmarks overhead with a single task
// (baseline for measuring ProgressGroup overhead).
func BenchmarkProgressGroup_SingleTask(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{Name: "task-1", Fn: func(ctx context.Context) error { time.Sleep(5 * time.Millisecond); return nil }},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)
		_ = pg.Run(context.Background(), tasks...)
	}
}

// BenchmarkProgressGroup_NoOp benchmarks overhead with no-op tasks
// (measures pure ProgressGroup coordination overhead without task work).
func BenchmarkProgressGroup_NoOp(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{Name: "task-1", Fn: func(ctx context.Context) error { return nil }},
		{Name: "task-2", Fn: func(ctx context.Context) error { return nil }},
		{Name: "task-3", Fn: func(ctx context.Context) error { return nil }},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)
		_ = pg.Run(context.Background(), tasks...)
	}
}

// BenchmarkProgressGroup_VaryingTaskDurations benchmarks tasks with different durations
// (simulates realistic workloads where some tasks complete faster than others).
func BenchmarkProgressGroup_VaryingTaskDurations(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{Name: "fast", Fn: func(ctx context.Context) error { time.Sleep(1 * time.Millisecond); return nil }},
		{Name: "medium", Fn: func(ctx context.Context) error { time.Sleep(10 * time.Millisecond); return nil }},
		{Name: "slow", Fn: func(ctx context.Context) error { time.Sleep(50 * time.Millisecond); return nil }},
		{Name: "fast-2", Fn: func(ctx context.Context) error { time.Sleep(2 * time.Millisecond); return nil }},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)
		_ = pg.Run(context.Background(), tasks...)
	}
}
