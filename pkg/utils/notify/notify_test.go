package notify_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
	"unicode"

	notify "github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
)

func TestWriteMessage_ErrorType(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	notify.WriteMessage(notify.Message{
		Type:    notify.ErrorType,
		Content: "test error",
		Writer:  &out,
	})

	got := out.String()
	want := "✗ test error\n"

	if got != want {
		t.Fatalf("output mismatch. want %q, got %q", want, got)
	}
}

func TestWriteMessage_ErrorType_WithFormatting(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	notify.WriteMessage(notify.Message{
		Type:    notify.ErrorType,
		Content: "error: %s (%d)",
		Args:    []any{"failed", 42},
		Writer:  &out,
	})

	got := out.String()
	want := "✗ error: failed (42)\n"

	if got != want {
		t.Fatalf("output mismatch. want %q, got %q", want, got)
	}
}

func TestWriteMessage_WarningType(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	notify.WriteMessage(notify.Message{
		Type:    notify.WarningType,
		Content: "test warning",
		Writer:  &out,
	})

	got := out.String()
	want := "⚠ test warning\n"

	if got != want {
		t.Fatalf("output mismatch. want %q, got %q", want, got)
	}
}

func TestWriteMessage_SuccessType(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "test success",
		Writer:  &out,
	})

	got := out.String()
	want := "✔ test success\n"

	if got != want {
		t.Fatalf("output mismatch. want %q, got %q", want, got)
	}
}

func TestWriteMessage_ActivityType(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "test activity",
		Writer:  &out,
	})

	got := out.String()
	want := "► test activity\n"

	if got != want {
		t.Fatalf("output mismatch. want %q, got %q", want, got)
	}
}

func TestWriteMessage_InfoType(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: "test info",
		Writer:  &out,
	})

	got := out.String()
	want := "ℹ test info\n"

	if got != want {
		t.Fatalf("output mismatch. want %q, got %q", want, got)
	}
}

func TestWriteMessage_MultiLineContentIndented(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "first line\nsecond line\n\nthird line",
		Writer:  &out,
	})

	got := out.String()
	want := "✔ first line\n  second line\n\n  third line\n"

	if got != want {
		t.Fatalf("output mismatch. want %q, got %q", want, got)
	}
}

func TestWriteMessage_TitleType(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "test title",
		Emoji:   "🚀",
		Writer:  &out,
	})

	got := out.String()
	want := "🚀 test title\n"

	if got != want {
		t.Fatalf("output mismatch. want %q, got %q", want, got)
	}
}

func TestWriteMessage_TitleType_DefaultEmoji(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "test title with default emoji",
		Writer:  &out,
	})

	got := out.String()
	want := "ℹ️ test title with default emoji\n"

	if got != want {
		t.Fatalf("output mismatch. want %q, got %q", want, got)
	}
}

func TestWriteMessage_WithTimer(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	tmr := timer.New()
	tmr.Start()

	time.Sleep(10 * time.Millisecond)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "operation complete",
		Timer:   tmr,
		Writer:  &out,
	})

	got := out.String()
	if !strings.HasPrefix(got, "✔ operation complete\n⏲ current: ") {
		t.Fatalf("output should start with success line and timing block, got %q", got)
	}

	if !strings.Contains(got, "\n  total:  ") {
		t.Fatalf("output should include total timing line, got %q", got)
	}
}

type fixedTimer struct {
	total time.Duration
	stage time.Duration
}

func (t *fixedTimer) Start() {}

func (t *fixedTimer) NewStage() {}

func (t *fixedTimer) GetTiming() (time.Duration, time.Duration) { return t.total, t.stage }

func (t *fixedTimer) Stop() {}

func TestWriteMessage_SuccessType_RendersTimingBlock_FormatAndPlacement(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	tmr := &fixedTimer{total: 3 * time.Second, stage: 500 * time.Millisecond}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "completion message",
		Timer:   tmr,
		Writer:  &out,
	})

	got := out.String()

	want := "✔ completion message\n⏲ current: 500ms\n  total:  3s\n"
	if got != want {
		t.Fatalf("output mismatch. want %q, got %q", want, got)
	}
}

func TestWriteMessage_ErrorType_DoesNotRenderTimingBlock(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	tmr := &fixedTimer{total: time.Second, stage: 10 * time.Millisecond}

	notify.WriteMessage(notify.Message{
		Type:    notify.ErrorType,
		Content: "test error",
		Timer:   tmr,
		Writer:  &out,
	})

	got := out.String()

	want := "✗ test error\n"
	if got != want {
		t.Fatalf("output mismatch. want %q, got %q", want, got)
	}
}

func TestWriteMessage_DefaultWriter(t *testing.T) {
	t.Parallel()

	// Test that nil writer defaults to stdout (just verify no panic)
	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: "test with default writer",
		// Writer is nil - should default to os.Stdout
	})
	// If we get here without panicking, test passes
}

type failingWriter struct{}

var errNotifyWriterFailed = errors.New("write failed")

func (f failingWriter) Write(_ []byte) (int, error) {
	return 0, errNotifyWriterFailed
}

func TestWriteMessage_HandleNotifyError(t *testing.T) {
	t.Parallel()

	origStderr := os.Stderr

	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	defer func() { _ = pipeReader.Close() }()

	os.Stderr = pipeWriter

	defer func() { os.Stderr = origStderr }()

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "should fallback",
		Writer:  failingWriter{},
	})

	_ = pipeWriter.Close()

	data, readErr := io.ReadAll(pipeReader)
	if readErr != nil {
		t.Fatalf("failed to read stderr: %v", readErr)
	}

	if !strings.Contains(string(data), "notify: failed to print message") {
		t.Fatalf("expected error log, got %q", string(data))
	}
}

func TestActivityMessage_MustBeLowercase(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		content     string
		shouldError bool
	}{
		{
			name:        "valid lowercase message",
			content:     "installing cilium",
			shouldError: false,
		},
		{
			name:        "valid lowercase with numbers",
			content:     "installing cni version 1.2.3",
			shouldError: false,
		},
		{
			name:        "valid lowercase with hyphens",
			content:     "awaiting metrics-server to be ready",
			shouldError: false,
		},
		{
			name:        "invalid uppercase component name",
			content:     "installing Cilium",
			shouldError: true,
		},
		{
			name:        "invalid uppercase acronym",
			content:     "CNI installed",
			shouldError: true,
		},
		{
			name:        "invalid mixed case",
			content:     "Installing Calico CNI",
			shouldError: true,
		},
		{
			name:        "invalid uppercase at start",
			content:     "Awaiting cilium to be ready",
			shouldError: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			hasUppercase := hasUppercaseLetters(testCase.content)

			if hasUppercase && !testCase.shouldError {
				t.Errorf("Expected no uppercase letters in %q but found some", testCase.content)
			}

			if !hasUppercase && testCase.shouldError {
				t.Errorf("Expected uppercase letters in %q but found none", testCase.content)
			}
		})
	}
}

func hasUppercaseLetters(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}

	return false
}

// =============================================================================
// Convenience Function Tests
// =============================================================================

func TestErrorf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
		args   []any
		want   string
	}{
		{
			name:   "simple message",
			format: "something went wrong",
			want:   "✗ something went wrong\n",
		},
		{
			name:   "formatted message",
			format: "failed to create %s: %d errors",
			args:   []any{"cluster", 3},
			want:   "✗ failed to create cluster: 3 errors\n",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			notify.Errorf(&buf, testCase.format, testCase.args...)

			if got := buf.String(); got != testCase.want {
				t.Errorf("Errorf() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestWarningf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
		args   []any
		want   string
	}{
		{
			name:   "simple message",
			format: "deprecated feature used",
			want:   "⚠ deprecated feature used\n",
		},
		{
			name:   "formatted message",
			format: "cluster %q may be unstable",
			args:   []any{"test-cluster"},
			want:   "⚠ cluster \"test-cluster\" may be unstable\n",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			notify.Warningf(&buf, testCase.format, testCase.args...)

			if got := buf.String(); got != testCase.want {
				t.Errorf("Warningf() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestActivityf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
		args   []any
		want   string
	}{
		{
			name:   "simple message",
			format: "installing cilium",
			want:   "► installing cilium\n",
		},
		{
			name:   "formatted message",
			format: "deploying %s to namespace %s",
			args:   []any{"app", "default"},
			want:   "► deploying app to namespace default\n",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			notify.Activityf(&buf, testCase.format, testCase.args...)

			if got := buf.String(); got != testCase.want {
				t.Errorf("Activityf() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestGeneratef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
		args   []any
		want   string
	}{
		{
			name:   "simple message",
			format: "kind.yaml",
			want:   "✚ kind.yaml\n",
		},
		{
			name:   "formatted message",
			format: "%s/%s",
			args:   []any{"k8s", "kustomization.yaml"},
			want:   "✚ k8s/kustomization.yaml\n",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			notify.Generatef(&buf, testCase.format, testCase.args...)

			if got := buf.String(); got != testCase.want {
				t.Errorf("Generatef() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestSuccessf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
		args   []any
		want   string
	}{
		{
			name:   "simple message",
			format: "cluster created",
			want:   "✔ cluster created\n",
		},
		{
			name:   "formatted message",
			format: "deployed %d replicas of %s",
			args:   []any{3, "nginx"},
			want:   "✔ deployed 3 replicas of nginx\n",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			notify.Successf(&buf, testCase.format, testCase.args...)

			if got := buf.String(); got != testCase.want {
				t.Errorf("Successf() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestSuccessWithTimerf(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	tmr := &fixedTimer{total: 5 * time.Second, stage: 2 * time.Second}

	notify.SuccessWithTimerf(&buf, tmr, "operation %s complete", "deploy")

	got := buf.String()
	want := "✔ operation deploy complete\n⏲ current: 2s\n  total:  5s\n"

	if got != want {
		t.Errorf("SuccessWithTimerf() = %q, want %q", got, want)
	}
}

func TestInfof(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
		args   []any
		want   string
	}{
		{
			name:   "simple message",
			format: "using context local",
			want:   "ℹ using context local\n",
		},
		{
			name:   "formatted message",
			format: "cluster %s has %d nodes",
			args:   []any{"prod", 5},
			want:   "ℹ cluster prod has 5 nodes\n",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			notify.Infof(&buf, testCase.format, testCase.args...)

			if got := buf.String(); got != testCase.want {
				t.Errorf("Infof() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestTitlef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		emoji  string
		format string
		args   []any
		want   string
	}{
		{
			name:   "with custom emoji",
			emoji:  "🚀",
			format: "deploying to production",
			want:   "🚀 deploying to production\n",
		},
		{
			name:   "with formatted message",
			emoji:  "📦",
			format: "installing %d components",
			args:   []any{5},
			want:   "📦 installing 5 components\n",
		},
		{
			name:   "with empty emoji uses default",
			emoji:  "",
			format: "status update",
			want:   "ℹ️ status update\n",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			notify.Titlef(&buf, testCase.emoji, testCase.format, testCase.args...)

			if got := buf.String(); got != testCase.want {
				t.Errorf("Titlef() = %q, want %q", got, testCase.want)
			}
		})
	}
}
