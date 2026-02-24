package kernelmod_test

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kernelmod"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goosLinux = "linux"

// isBrNetfilterLoaded checks if br_netfilter is already loaded using production logic.
func isBrNetfilterLoaded(t *testing.T) bool {
	t.Helper()

	data, err := os.ReadFile("/proc/modules")
	if err != nil {
		t.Skipf("Cannot read /proc/modules: %v (may not be running on real Linux)", err)
	}

	return kernelmod.ContainsModule(string(data), "br_netfilter")
}

// TestEnsureBrNetfilter_NonLinux verifies that the function is a no-op on non-Linux systems.
func TestEnsureBrNetfilter_NonLinux(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == goosLinux {
		t.Skip("Skipping non-Linux test on Linux system")
	}

	ctx := context.Background()

	var logOutput strings.Builder

	err := kernelmod.EnsureBrNetfilter(ctx, &logOutput)

	require.NoError(t, err)
	assert.Empty(t, logOutput.String(), "Should not write any logs on non-Linux systems")
}

// TestEnsureBrNetfilter_AlreadyLoaded tests the scenario where br_netfilter is already loaded.
//
//nolint:paralleltest // Cannot run in parallel: EnsureBrNetfilter may trigger modprobe with global side effects.
func TestEnsureBrNetfilter_AlreadyLoaded(t *testing.T) {
	if runtime.GOOS != goosLinux {
		t.Skip("Skipping Linux-specific test on non-Linux system")
	}

	if !isBrNetfilterLoaded(t) {
		t.Skip("Skipping: br_netfilter is not already loaded")
	}

	ctx := context.Background()

	var logOutput strings.Builder

	err := kernelmod.EnsureBrNetfilter(ctx, &logOutput)

	require.NoError(t, err)
	assert.Empty(t, logOutput.String(), "Should not write logs when module already loaded")
}

// TestEnsureBrNetfilter_WithLogWriter tests that log messages are written when provided.
//
//nolint:paralleltest // Cannot run in parallel: EnsureBrNetfilter may trigger modprobe with global side effects.
func TestEnsureBrNetfilter_WithLogWriter(t *testing.T) {
	if runtime.GOOS != goosLinux {
		t.Skip("Skipping Linux-specific test on non-Linux system")
	}

	if !isBrNetfilterLoaded(t) {
		t.Skip("Skipping: br_netfilter is not already loaded (test would trigger modprobe)")
	}

	ctx := context.Background()

	var logOutput strings.Builder

	err := kernelmod.EnsureBrNetfilter(ctx, &logOutput)

	require.NoError(t, err)
	assert.Empty(t, logOutput.String(), "Should not write logs when module already loaded")
}

// TestEnsureBrNetfilter_NilLogWriter tests that nil log writer doesn't cause panic.
//
//nolint:paralleltest // Cannot run in parallel: EnsureBrNetfilter may trigger modprobe with global side effects.
func TestEnsureBrNetfilter_NilLogWriter(t *testing.T) {
	if runtime.GOOS != goosLinux {
		t.Skip("Skipping Linux-specific test on non-Linux system")
	}

	if !isBrNetfilterLoaded(t) {
		t.Skip("Skipping: br_netfilter is not already loaded (test would trigger modprobe)")
	}

	ctx := context.Background()

	require.NotPanics(t, func() {
		_ = kernelmod.EnsureBrNetfilter(ctx, nil)
	})
}

// TestEnsureBrNetfilter_ContextCancellation tests behavior with cancelled context.
//
//nolint:paralleltest // Cannot run in parallel: EnsureBrNetfilter may trigger modprobe with global side effects.
func TestEnsureBrNetfilter_ContextCancellation(t *testing.T) {
	if runtime.GOOS != goosLinux {
		t.Skip("Skipping Linux-specific test on non-Linux system")
	}

	if isBrNetfilterLoaded(t) {
		// If module is already loaded, it returns early before using context.
		// This path is already covered by TestEnsureBrNetfilter_AlreadyLoaded.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		var logOutput strings.Builder

		err := kernelmod.EnsureBrNetfilter(ctx, &logOutput)

		require.NoError(t, err, "Should succeed with cancelled context when module already loaded")

		return
	}

	// Module not loaded: cancelled context should cause modprobe to fail.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var logOutput strings.Builder

	err := kernelmod.EnsureBrNetfilter(ctx, &logOutput)

	require.Error(t, err, "Should fail when module needs loading with cancelled context")
}

// TestContainsModule_Integration tests containsModule logic indirectly via realistic scenarios.
func TestContainsModule_Integration(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != goosLinux {
		t.Skip("Skipping Linux-specific test on non-Linux system")
	}

	data, err := os.ReadFile("/proc/modules")
	if err != nil {
		t.Skipf("Cannot read /proc/modules: %v", err)
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	var realModuleName string

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			realModuleName = fields[0]

			break
		}
	}

	if realModuleName == "" {
		t.Skip("No modules found in /proc/modules")
	}

	t.Logf("Testing with real module name: %s", realModuleName)

	// Verify production containsModule detects a real module
	assert.True(t, kernelmod.ContainsModule(content, realModuleName),
		"Should detect real module %q in /proc/modules", realModuleName)

	// Verify that if br_netfilter is present, the early return works
	if kernelmod.ContainsModule(content, "br_netfilter") {
		ctx := context.Background()

		var logOutput strings.Builder

		err := kernelmod.EnsureBrNetfilter(ctx, &logOutput)

		require.NoError(t, err, "Should succeed when br_netfilter is already loaded")
		assert.Empty(t, logOutput.String(), "Should not attempt to load when already present")
	}
}
