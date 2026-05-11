package fsutil_test

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

// BenchmarkExpandHomePath_HomePrefix benchmarks the ~/ expansion path.
// The cache is warmed before timing begins so every iteration measures the
// cached fast path exclusively.
func BenchmarkExpandHomePath_HomePrefix(b *testing.B) {
	usr, err := user.Current()
	if err != nil {
		b.Skipf("skipping: cannot get current user: %v", err)
	}

	expected := filepath.Join(usr.HomeDir, "some", "nested", "dir")

	// Warm the sync.Once cache so the timed loop only measures the fast path.
	_, err = fsutil.ExpandHomePath("~/warmup")
	if err != nil {
		b.Fatalf("cache warmup failed: %v", err)
	}

	b.ResetTimer()

	for b.Loop() {
		got, err := fsutil.ExpandHomePath("~/some/nested/dir")
		if err != nil || got != expected {
			b.Fatalf("unexpected result: got=%q err=%v", got, err)
		}
	}
}

// BenchmarkExpandHomePath_Absolute benchmarks the already-absolute fast path
// (no home-directory lookup required).
func BenchmarkExpandHomePath_Absolute(b *testing.B) {
	path := filepath.Join(os.TempDir(), "some", "config.yaml")

	b.ResetTimer()

	for b.Loop() {
		got, err := fsutil.ExpandHomePath(path)
		if err != nil || got != path {
			b.Fatalf("unexpected result: got=%q err=%v", got, err)
		}
	}
}

// BenchmarkExpandHomePath_Relative benchmarks the relative-to-absolute
// conversion path (no home-directory lookup required).
func BenchmarkExpandHomePath_Relative(b *testing.B) {
	b.ResetTimer()

	for b.Loop() {
		_, err := fsutil.ExpandHomePath("some/relative/path")
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}
