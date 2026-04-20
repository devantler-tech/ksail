package notify_test

import (
	"bytes"
	"io"
	"testing"
	"time"

	notify "github.com/devantler-tech/ksail/v7/pkg/notify"
)

// BenchmarkWriteMessage_SingleLine benchmarks a simple single-line message with no format args.
func BenchmarkWriteMessage_SingleLine(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "installing cilium",
			Writer:  io.Discard,
		})
	}
}

// BenchmarkWriteMessage_WithArgs benchmarks a message with format arguments.
func BenchmarkWriteMessage_WithArgs(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: "failed to create cluster %q: %d errors",
			Args:    []any{"prod", 3},
			Writer:  io.Discard,
		})
	}
}

// BenchmarkWriteMessage_Multiline benchmarks a multiline message that triggers indent computation.
func BenchmarkWriteMessage_Multiline(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		notify.WriteMessage(notify.Message{
			Type:    notify.SuccessType,
			Content: "cluster created\nnode 1 ready\nnode 2 ready\nnode 3 ready",
			Writer:  io.Discard,
		})
	}
}

// BenchmarkWriteMessage_WithTimer benchmarks a success message that includes timer output.
func BenchmarkWriteMessage_WithTimer(b *testing.B) {
	b.ReportAllocs()

	tmr := &fixedBenchTimer{total: 5 * time.Second, stage: 2 * time.Second}

	for b.Loop() {
		notify.WriteMessage(notify.Message{
			Type:    notify.SuccessType,
			Content: "cluster created",
			Timer:   tmr,
			Writer:  io.Discard,
		})
	}
}

// BenchmarkWriteMessage_AllTypes benchmarks all message types to exercise every code path.
func BenchmarkWriteMessage_AllTypes(b *testing.B) {
	b.ReportAllocs()

	writer := &bytes.Buffer{}

	types := []notify.MessageType{
		notify.ErrorType,
		notify.WarningType,
		notify.ActivityType,
		notify.GenerateType,
		notify.SuccessType,
		notify.InfoType,
		notify.TitleType,
	}

	for b.Loop() {
		for _, msgType := range types {
			writer.Reset()

			notify.WriteMessage(notify.Message{
				Type:    msgType,
				Content: "benchmark message",
				Emoji:   "🚀",
				Writer:  writer,
			})
		}
	}
}

type fixedBenchTimer struct {
	total time.Duration
	stage time.Duration
}

func (t *fixedBenchTimer) Start()    {}
func (t *fixedBenchTimer) NewStage() {}
func (t *fixedBenchTimer) Stop()     {}

func (t *fixedBenchTimer) GetTiming() (time.Duration, time.Duration) {
	return t.total, t.stage
}
