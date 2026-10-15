package codeqlprofile_test

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/codeqlprofile"
)

func TestCountExtractedFilesCountsDistinctNativeCompletions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "pkg", "file.go")
	input := "2026/10/14 10:30:12 Done extracting " + path + " (12ms)\n" +
		"2026/10/14 10:30:13.123 Done extracting " + path + " (20ms)\n" +
		"Done extracting " + filepath.Join(root, "desktop", "main.go") + " (5ms)\n" +
		"Done extracting /outside/dependency.go (1ms)\n" +
		"Done extracting " + filepath.Join(root, "go.mod") + " (1ms)\n" +
		"warning: Done extracting " + filepath.Join(root, "fake.go") + " (1ms)\n" +
		"Done extracting " + filepath.Join(root, "incomplete.go") + " (\n"

	count, err := codeqlprofile.CountExtractedFiles(root, strings.NewReader(input))
	if err != nil || count != 2 {
		t.Fatalf("native completions: got %d, %v; want 2", count, err)
	}
}

type partialExtractorLog struct{ done bool }

var errFixtureLog = errors.New("fixture log read failed")

func (r *partialExtractorLog) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true

		return copy(p, "Done extracting /source/file.go (1ms)\n"), nil
	}

	return 0, errFixtureLog
}

func TestCountExtractedFilesRejectsPartialFailedReads(t *testing.T) {
	t.Parallel()

	for _, reader := range []io.Reader{&partialExtractorLog{}, strings.NewReader(strings.Repeat("x", 2*1024*1024))} {
		_, err := codeqlprofile.CountExtractedFiles("/source", reader)
		if err == nil {
			t.Fatal("an incomplete log was accepted")
		}
	}
}
