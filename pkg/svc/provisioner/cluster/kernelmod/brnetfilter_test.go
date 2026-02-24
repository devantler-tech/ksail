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
func TestEnsureBrNetfilter_AlreadyLoaded(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != goosLinux {
		t.Skip("Skipping Linux-specific test on non-Linux system")
	}

	// Check if /proc/modules exists and is readable
	data, err := os.ReadFile("/proc/modules")
	if err != nil {
		t.Skipf("Cannot read /proc/modules: %v (may not be running on real Linux)", err)
	}

	// Check if br_netfilter is already loaded in the real system
	alreadyLoaded := strings.Contains(string(data), "br_netfilter")

	ctx := context.Background()

	var logOutput strings.Builder

	err = kernelmod.EnsureBrNetfilter(ctx, &logOutput)

	if alreadyLoaded {
		// If module was already loaded, function should succeed without trying to load
		require.NoError(t, err)
		assert.Empty(t, logOutput.String(), "Should not write logs when module already loaded")
	} else {
		// If module was not loaded, function will try to load it
		// This may fail in containers or CI without proper permissions
		// We can't assert on the outcome without knowing the environment
		t.Logf("Module not pre-loaded, load attempt result: err=%v, log=%q",
			err, logOutput.String())
	}
}

// TestEnsureBrNetfilter_WithLogWriter tests that log messages are written when provided.
func TestEnsureBrNetfilter_WithLogWriter(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != goosLinux {
		t.Skip("Skipping Linux-specific test on non-Linux system")
	}

	// Check if /proc/modules exists
	_, err := os.ReadFile("/proc/modules")
	if err != nil {
		t.Skipf("Cannot read /proc/modules: %v (may not be running on real Linux)", err)
	}

	ctx := context.Background()

	var logOutput strings.Builder

	// Run the function
	_ = kernelmod.EnsureBrNetfilter(ctx, &logOutput)

	// We expect either:
	// 1. Empty log (module already loaded, early return)
	// 2. "Loading br_netfilter" message (module not loaded, attempting load)
	// 3. "Successfully loaded" message (load succeeded)
	log := logOutput.String()
	if log != "" {
		assert.Contains(t, log, "br_netfilter", "Log should mention br_netfilter module")
	}
}

// TestEnsureBrNetfilter_NilLogWriter tests that nil log writer doesn't cause panic.
func TestEnsureBrNetfilter_NilLogWriter(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != goosLinux {
		t.Skip("Skipping Linux-specific test on non-Linux system")
	}

	// Check if /proc/modules exists
	_, err := os.ReadFile("/proc/modules")
	if err != nil {
		t.Skipf("Cannot read /proc/modules: %v (may not be running on real Linux)", err)
	}

	ctx := context.Background()

	// Should not panic with nil log writer
	require.NotPanics(t, func() {
		_ = kernelmod.EnsureBrNetfilter(ctx, nil)
	})
}

// TestEnsureBrNetfilter_ContextCancellation tests behavior with cancelled context.
func TestEnsureBrNetfilter_ContextCancellation(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != goosLinux {
		t.Skip("Skipping Linux-specific test on non-Linux system")
	}

	// Check if /proc/modules exists
	_, err := os.ReadFile("/proc/modules")
	if err != nil {
		t.Skipf("Cannot read /proc/modules: %v (may not be running on real Linux)", err)
	}

	// Create an already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var logOutput strings.Builder

	err = kernelmod.EnsureBrNetfilter(ctx, &logOutput)

	// If module is already loaded, should succeed even with cancelled context
	// If module needs loading, should fail due to context cancellation
	// We can't assert definitively without knowing module state
	t.Logf("Context cancellation test result: err=%v", err)
}

// TestContainsModule_Integration tests containsModule logic indirectly via realistic scenarios.
func TestContainsModule_Integration(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != goosLinux {
		t.Skip("Skipping Linux-specific test on non-Linux system")
	}

	// Read real /proc/modules to test realistic parsing
	data, err := os.ReadFile("/proc/modules")
	if err != nil {
		t.Skipf("Cannot read /proc/modules: %v", err)
	}

	// Test with real module data
	content := string(data)
	lines := strings.Split(content, "\n")

	// Find at least one real module name from the first line
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

	// Verify that if a module appears in /proc/modules, the early return works
	if strings.Contains(content, "br_netfilter") {
		ctx := context.Background()

		var logOutput strings.Builder

		err := kernelmod.EnsureBrNetfilter(ctx, &logOutput)

		require.NoError(t, err, "Should succeed when br_netfilter is already loaded")
		assert.Empty(t, logOutput.String(), "Should not attempt to load when already present")
	}
}

// TestEnsureBrNetfilter_ErrorScenarios tests various error conditions.
func TestEnsureBrNetfilter_ErrorScenarios(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != goosLinux {
		t.Skip("Skipping Linux-specific test on non-Linux system")
	}

	// This test documents expected behavior in error scenarios
	// In containers without CAP_SYS_MODULE or in CI environments,
	// the function may fail to load the module
	ctx := context.Background()

	var logOutput strings.Builder

	err := kernelmod.EnsureBrNetfilter(ctx, &logOutput)

	// Document the actual behavior
	if err != nil {
		t.Logf("Expected failure in restricted environment: %v", err)
		assert.Contains(t, err.Error(), "br_netfilter", "Error should mention the module name")
		assert.Contains(t, err.Error(), "failed to load", "Error should indicate load failure")
	} else {
		t.Logf("Successfully loaded or module already present")
	}
}
