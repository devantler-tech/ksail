package fsutil_test

import (
	"os/user"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

// BenchmarkExpandHomePath_HomePrefix benchmarks the ~/ expansion path, which
// exercises the home-directory lookup. After the first call the result is
// served from the sync.Once cache, so subsequent iterations measure the
// cached fast path.
func BenchmarkExpandHomePath_HomePrefix(b *testing.B) {
	usr, err := user.Current()
	if err != nil {
		b.Skipf("skipping: cannot get current user: %v", err)
	}

	expected := filepath.Join(usr.HomeDir, "some", "nested", "dir")

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
	path := "/tmp/some/config.yaml"

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
