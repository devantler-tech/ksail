package notify_test

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
	fcolor "github.com/fatih/color"
)

// NOTE: These benchmarks use io.Discard and bytes.Buffer as writers, which are
// non-TTY. This means only the CI output path (runCI) is benchmarked, not the
// interactive TTY path (runInteractive) with animated spinners. Benchmarking the
// TTY path would require a real terminal file descriptor.

// BenchmarkProgressGroup_Sequential benchmarks progress group when work runs sequentially
// within a single task (simulates scenarios where tasks cannot be parallelized).
func BenchmarkProgressGroup_Sequential(b *testing.B) {
	// Use a no-op writer to avoid I/O overhead in benchmarks
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{
			Name: "sequential-task",
			Fn: func(_ context.Context) error {
				time.Sleep(10 * time.Millisecond)
				time.Sleep(10 * time.Millisecond)
				time.Sleep(10 * time.Millisecond)

				return nil
			},
		},
	}

	b.ResetTimer()

	for range b.N {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)

		err := pg.Run(context.Background(), tasks...)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProgressGroup_Parallel_Fast benchmarks parallel execution with fast tasks
// (simulates many quick operations like validation checks).
func BenchmarkProgressGroup_Parallel_Fast(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{
			Name: "task-1",
			Fn: func(_ context.Context) error {
				time.Sleep(1 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "task-2",
			Fn: func(_ context.Context) error {
				time.Sleep(1 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "task-3",
			Fn: func(_ context.Context) error {
				time.Sleep(1 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "task-4",
			Fn: func(_ context.Context) error {
				time.Sleep(1 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "task-5",
			Fn: func(_ context.Context) error {
				time.Sleep(1 * time.Millisecond)

				return nil
			},
		},
	}

	b.ResetTimer()

	for range b.N {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)

		err := pg.Run(context.Background(), tasks...)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProgressGroup_Parallel_Slow benchmarks parallel execution with slower tasks
// (simulates real-world scenarios like component installation).
func BenchmarkProgressGroup_Parallel_Slow(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{
			Name: "task-1",
			Fn: func(_ context.Context) error {
				time.Sleep(50 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "task-2",
			Fn: func(_ context.Context) error {
				time.Sleep(50 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "task-3",
			Fn: func(_ context.Context) error {
				time.Sleep(50 * time.Millisecond)

				return nil
			},
		},
	}

	b.ResetTimer()

	for range b.N {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)

		err := pg.Run(context.Background(), tasks...)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProgressGroup_ManyTasks benchmarks progress group with many parallel tasks
// (simulates high concurrency scenarios like batch operations).
func BenchmarkProgressGroup_ManyTasks(b *testing.B) {
	writer := io.Discard

	// Create 20 tasks
	tasks := make([]notify.ProgressTask, 20)
	for i := range 20 {
		tasks[i] = notify.ProgressTask{
			Name: string(rune('a' + i)),
			Fn: func(_ context.Context) error {
				time.Sleep(5 * time.Millisecond)

				return nil
			},
		}
	}

	b.ResetTimer()

	for range b.N {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)

		err := pg.Run(context.Background(), tasks...)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProgressGroup_WithTimer benchmarks progress group with timer overhead.
func BenchmarkProgressGroup_WithTimer(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{
			Name: "task-1",
			Fn: func(_ context.Context) error {
				time.Sleep(5 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "task-2",
			Fn: func(_ context.Context) error {
				time.Sleep(5 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "task-3",
			Fn: func(_ context.Context) error {
				time.Sleep(5 * time.Millisecond)

				return nil
			},
		},
	}

	b.ResetTimer()

	for range b.N {
		tmr := timer.New()
		tmr.Start()
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer, notify.WithTimer(tmr))

		err := pg.Run(context.Background(), tasks...)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProgressGroup_CI_Mode benchmarks CI mode (no TTY, simpler output).
// This simulates the non-interactive output path used in CI/CD pipelines.
func BenchmarkProgressGroup_CI_Mode(b *testing.B) {
	// Use a buffer to simulate CI output (not a TTY)
	var buf bytes.Buffer

	// Force color off to accurately model CI/piped output
	origNoColor := fcolor.NoColor
	fcolor.NoColor = true

	b.Cleanup(func() {
		fcolor.NoColor = origNoColor
	})

	tasks := []notify.ProgressTask{
		{
			Name: "task-1",
			Fn: func(_ context.Context) error {
				time.Sleep(5 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "task-2",
			Fn: func(_ context.Context) error {
				time.Sleep(5 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "task-3",
			Fn: func(_ context.Context) error {
				time.Sleep(5 * time.Millisecond)

				return nil
			},
		},
	}

	b.ResetTimer()

	for range b.N {
		buf.Reset()
		pg := notify.NewProgressGroup("Benchmarking", "⏱", &buf)

		err := pg.Run(context.Background(), tasks...)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProgressGroup_CustomLabels benchmarks progress group with custom labels.
func BenchmarkProgressGroup_CustomLabels(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{
			Name: "task-1",
			Fn: func(_ context.Context) error {
				time.Sleep(5 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "task-2",
			Fn: func(_ context.Context) error {
				time.Sleep(5 * time.Millisecond)

				return nil
			},
		},
	}

	labels := notify.InstallingLabels()

	b.ResetTimer()

	for range b.N {
		pg := notify.NewProgressGroup("Installing", "📦", writer, notify.WithLabels(labels))

		err := pg.Run(context.Background(), tasks...)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProgressGroup_SingleTask benchmarks overhead with a single task
// (baseline for measuring ProgressGroup overhead).
func BenchmarkProgressGroup_SingleTask(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{
			Name: "task-1",
			Fn: func(_ context.Context) error {
				time.Sleep(5 * time.Millisecond)

				return nil
			},
		},
	}

	b.ResetTimer()

	for range b.N {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)

		err := pg.Run(context.Background(), tasks...)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProgressGroup_NoOp benchmarks overhead with no-op tasks
// (measures pure ProgressGroup coordination overhead without task work).
func BenchmarkProgressGroup_NoOp(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{Name: "task-1", Fn: func(_ context.Context) error { return nil }},
		{Name: "task-2", Fn: func(_ context.Context) error { return nil }},
		{Name: "task-3", Fn: func(_ context.Context) error { return nil }},
	}

	b.ResetTimer()

	for range b.N {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)

		err := pg.Run(context.Background(), tasks...)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProgressGroup_VaryingTaskDurations benchmarks tasks with different durations
// (simulates realistic workloads where some tasks complete faster than others).
func BenchmarkProgressGroup_VaryingTaskDurations(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{
			Name: "fast",
			Fn: func(_ context.Context) error {
				time.Sleep(1 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "medium",
			Fn: func(_ context.Context) error {
				time.Sleep(10 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "slow",
			Fn: func(_ context.Context) error {
				time.Sleep(50 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "fast-2",
			Fn: func(_ context.Context) error {
				time.Sleep(2 * time.Millisecond)

				return nil
			},
		},
	}

	b.ResetTimer()

	for range b.N {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)

		err := pg.Run(context.Background(), tasks...)
		if err != nil {
			b.Fatal(err)
		}
	}
}
