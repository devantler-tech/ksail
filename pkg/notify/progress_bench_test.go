package notify_test

import (
	"bytes"
	"context"
	"io"
	"runtime"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	fcolor "github.com/fatih/color"
)

// NOTE: These benchmarks use io.Discard and bytes.Buffer as writers, which are
// non-TTY. This means only the CI output path (runCI) is benchmarked, not the
// interactive TTY path (runInteractive) with animated spinners. Benchmarking the
// TTY path would require a real terminal file descriptor.

// busyWork performs deterministic CPU-bound work to simulate task duration
// without sleeping. Uses an LCG (linear congruential generator) to keep the
// CPU busy. The actual wall-clock cost of a given iteration count depends on
// the CPU, Go version, and compiler optimizations, so it should not be treated
// as a precise time duration.
func busyWork(iterations int) {
	x := uint64(1)
	for range iterations {
		x = x*6364136223846793005 + 1442695040888963407
	}

	runtime.KeepAlive(x)
}

// BenchmarkProgressGroup_Sequential benchmarks progress group when work runs sequentially
// within a single task (simulates scenarios where tasks cannot be parallelized).
func BenchmarkProgressGroup_Sequential(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{
			Name: "sequential-task",
			Fn: func(_ context.Context) error {
				busyWork(10000)
				busyWork(10000)
				busyWork(10000)

				return nil
			},
		},
	}

	b.ReportAllocs()
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
				busyWork(1000)

				return nil
			},
		},
		{
			Name: "task-2",
			Fn: func(_ context.Context) error {
				busyWork(1000)

				return nil
			},
		},
		{
			Name: "task-3",
			Fn: func(_ context.Context) error {
				busyWork(1000)

				return nil
			},
		},
		{
			Name: "task-4",
			Fn: func(_ context.Context) error {
				busyWork(1000)

				return nil
			},
		},
		{
			Name: "task-5",
			Fn: func(_ context.Context) error {
				busyWork(1000)

				return nil
			},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)

		err := pg.Run(context.Background(), tasks...)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProgressGroup_Parallel_Slow benchmarks parallel execution with heavier tasks
// (simulates real-world scenarios like component installation).
func BenchmarkProgressGroup_Parallel_Slow(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{
			Name: "task-1",
			Fn: func(_ context.Context) error {
				busyWork(50000)

				return nil
			},
		},
		{
			Name: "task-2",
			Fn: func(_ context.Context) error {
				busyWork(50000)

				return nil
			},
		},
		{
			Name: "task-3",
			Fn: func(_ context.Context) error {
				busyWork(50000)

				return nil
			},
		},
	}

	b.ReportAllocs()
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

	tasks := make([]notify.ProgressTask, 20)
	for i := range 20 {
		tasks[i] = notify.ProgressTask{
			Name: string(rune('a' + i)),
			Fn: func(_ context.Context) error {
				busyWork(5000)

				return nil
			},
		}
	}

	b.ReportAllocs()
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
				busyWork(5000)

				return nil
			},
		},
		{
			Name: "task-2",
			Fn: func(_ context.Context) error {
				busyWork(5000)

				return nil
			},
		},
		{
			Name: "task-3",
			Fn: func(_ context.Context) error {
				busyWork(5000)

				return nil
			},
		},
	}

	b.ReportAllocs()
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
	var buf bytes.Buffer

	origNoColor := fcolor.NoColor
	fcolor.NoColor = true

	b.Cleanup(func() {
		fcolor.NoColor = origNoColor
	})

	tasks := []notify.ProgressTask{
		{
			Name: "task-1",
			Fn: func(_ context.Context) error {
				busyWork(5000)

				return nil
			},
		},
		{
			Name: "task-2",
			Fn: func(_ context.Context) error {
				busyWork(5000)

				return nil
			},
		},
		{
			Name: "task-3",
			Fn: func(_ context.Context) error {
				busyWork(5000)

				return nil
			},
		},
	}

	b.ReportAllocs()
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
				busyWork(5000)

				return nil
			},
		},
		{
			Name: "task-2",
			Fn: func(_ context.Context) error {
				busyWork(5000)

				return nil
			},
		},
	}

	labels := notify.InstallingLabels()

	b.ReportAllocs()
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
				busyWork(5000)

				return nil
			},
		},
	}

	b.ReportAllocs()
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

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)

		err := pg.Run(context.Background(), tasks...)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProgressGroup_VaryingTaskDurations benchmarks tasks with different workloads
// (simulates realistic workloads where some tasks complete faster than others).
func BenchmarkProgressGroup_VaryingTaskDurations(b *testing.B) {
	writer := io.Discard

	tasks := []notify.ProgressTask{
		{
			Name: "fast",
			Fn: func(_ context.Context) error {
				busyWork(1000)

				return nil
			},
		},
		{
			Name: "medium",
			Fn: func(_ context.Context) error {
				busyWork(10000)

				return nil
			},
		},
		{
			Name: "slow",
			Fn: func(_ context.Context) error {
				busyWork(50000)

				return nil
			},
		},
		{
			Name: "fast-2",
			Fn: func(_ context.Context) error {
				busyWork(2000)

				return nil
			},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		pg := notify.NewProgressGroup("Benchmarking", "⏱", writer)

		err := pg.Run(context.Background(), tasks...)
		if err != nil {
			b.Fatal(err)
		}
	}
}
