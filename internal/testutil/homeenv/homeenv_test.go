package homeenv_test

import (
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/testutil/homeenv"
)

// TestIsolateRedirectsAndRestoresHome verifies that Isolate points the home
// environment variables (HOME and USERPROFILE) at a fresh directory and that
// the returned cleanup restores their original state.
//
//nolint:paralleltest // Mutates the process environment; cannot run in parallel.
func TestIsolateRedirectsAndRestoresHome(t *testing.T) {
	type envState struct {
		value string
		set   bool
	}

	// os.UserHomeDir consults HOME on unix/darwin and USERPROFILE on Windows;
	// Isolate must redirect and restore both.
	homeEnvVars := []string{"HOME", "USERPROFILE"}

	orig := make(map[string]envState, len(homeEnvVars))
	for _, key := range homeEnvVars {
		value, set := os.LookupEnv(key)
		orig[key] = envState{value: value, set: set}
	}

	cleanup := homeenv.Isolate()

	isolated, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir after Isolate: %v", err)
	}

	// Every home variable must point at the same fresh directory.
	for _, key := range homeEnvVars {
		got := os.Getenv(key)
		if got != isolated {
			t.Fatalf("Isolate set %s=%q, want the isolated home %q", key, got, isolated)
		}
	}

	_, statErr := os.Stat(isolated)
	if statErr != nil {
		t.Fatalf("isolated home %q is not a usable directory: %v", isolated, statErr)
	}

	cleanup()

	// Cleanup must restore every home variable to its original state.
	for _, key := range homeEnvVars {
		want := orig[key]

		got, gotSet := os.LookupEnv(key)
		if gotSet != want.set || got != want.value {
			t.Fatalf(
				"cleanup did not restore %s: got set=%v val=%q, want set=%v val=%q",
				key, gotSet, got, want.set, want.value,
			)
		}
	}

	_, removedErr := os.Stat(isolated)
	if !os.IsNotExist(removedErr) {
		t.Fatalf("cleanup did not remove isolated home %q (stat err: %v)", isolated, removedErr)
	}
}
