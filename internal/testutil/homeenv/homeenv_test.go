package homeenv_test

import (
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/testutil/homeenv"
)

// TestIsolateRedirectsAndRestoresHome verifies that Isolate points
// os.UserHomeDir() at a fresh directory and that the returned cleanup restores
// the original environment.
//
//nolint:paralleltest // Mutates the process environment; cannot run in parallel.
func TestIsolateRedirectsAndRestoresHome(t *testing.T) {
	origValue, origHad := os.LookupEnv("HOME")

	cleanup := homeenv.Isolate()

	isolated, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir after Isolate: %v", err)
	}

	if origHad && isolated == origValue {
		t.Fatalf("Isolate did not redirect HOME away from %q", origValue)
	}

	_, statErr := os.Stat(isolated)
	if statErr != nil {
		t.Fatalf("isolated home %q is not a usable directory: %v", isolated, statErr)
	}

	cleanup()

	afterValue, afterHad := os.LookupEnv("HOME")
	if afterHad != origHad || afterValue != origValue {
		t.Fatalf(
			"cleanup did not restore HOME: got had=%v val=%q, want had=%v val=%q",
			afterHad, afterValue, origHad, origValue,
		)
	}

	_, removedErr := os.Stat(isolated)
	if !os.IsNotExist(removedErr) {
		t.Fatalf("cleanup did not remove isolated home %q (stat err: %v)", isolated, removedErr)
	}
}
