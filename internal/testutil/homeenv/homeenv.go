// Package homeenv provides test helpers that redirect the user's home
// directory to a throwaway location so tests never read from or write to the
// developer's real ~/.kube/config, ~/.ksail/, or any other home-derived path.
//
// Many code paths resolve files via os.UserHomeDir() (kubeconfig defaults,
// ~/.ksail/switch-history.json, ~/.ksail/clusters/ state). Without isolation,
// tests that exercise those paths mutate the developer's real configuration.
package homeenv

import (
	"fmt"
	"os"
	"testing"
)

// Isolate points the home directory at a fresh temporary directory and returns
// a cleanup function that restores the previous environment and removes the
// temporary directory. It is intended for use from TestMain, where *testing.T
// is unavailable, and is safe to use with parallel tests because the
// environment is mutated once before any test runs.
func Isolate() func() {
	dir, err := os.MkdirTemp("", "ksail-test-home-")
	if err != nil {
		panic(fmt.Sprintf("homeenv: create temp home: %v", err))
	}

	// os.UserHomeDir consults HOME on unix/darwin and USERPROFILE on Windows.
	homeVars := []string{"HOME", "USERPROFILE"}
	restore := make([]func(), 0, len(homeVars))

	for _, key := range homeVars {
		prev, had := os.LookupEnv(key)

		err = os.Setenv(key, dir)
		if err != nil {
			panic(fmt.Sprintf("homeenv: set %s: %v", key, err))
		}

		restore = append(restore, func() {
			if had {
				_ = os.Setenv(key, prev)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}

	return func() {
		for _, fn := range restore {
			fn()
		}

		_ = os.RemoveAll(dir)
	}
}

// Run isolates the home directory for the duration of the test suite and
// returns the suite's exit code. Use it from packages whose TestMain has no
// other responsibilities:
//
//	func TestMain(m *testing.M) { os.Exit(homeenv.Run(m)) }
func Run(m *testing.M) int {
	cleanup := Isolate()
	defer cleanup()

	return m.Run()
}
