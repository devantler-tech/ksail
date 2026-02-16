package kernelmod

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// EnsureBrNetfilter loads the br_netfilter kernel module if not already loaded.
// On Linux, this is required for Docker bridge networking features.
// On macOS and Windows, Docker Desktop handles this automatically via its Linux VM.
//
// Parameters:
//   - ctx: context for command execution
//   - logWriter: optional writer for status messages (can be nil)
//
// Returns an error if the module cannot be loaded on Linux systems.
func EnsureBrNetfilter(ctx context.Context, logWriter io.Writer) error {
	// Only needed on Linux - Docker Desktop on macOS/Windows handles this in its VM
	if runtime.GOOS != "linux" {
		return nil
	}

	// Check if br_netfilter is already loaded by reading /proc/modules
	data, err := os.ReadFile("/proc/modules")
	if err == nil {
		// Check if br_netfilter is in the loaded modules list
		if containsModule(string(data), "br_netfilter") {
			return nil // Already loaded
		}
	}

	// Try to load the module using modprobe
	if logWriter != nil {
		_, _ = fmt.Fprintf(logWriter, "Loading br_netfilter kernel module...\n")
	}

	cmd := exec.CommandContext(ctx, "modprobe", "br_netfilter")

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try with sudo if direct modprobe fails (user may not have CAP_SYS_MODULE).
		// Use -n (non-interactive) so it fails fast in CI/headless environments
		// instead of blocking on a password prompt.
		sudoCmd := exec.CommandContext(ctx, "sudo", "-n", "modprobe", "br_netfilter")

		sudoOutput, sudoErr := sudoCmd.CombinedOutput()
		if sudoErr != nil {
			return fmt.Errorf(
				"failed to load br_netfilter kernel module (modprobe failed: %w, sudo modprobe failed: %w, output: %s)",
				err,
				sudoErr,
				string(append(output, sudoOutput...)),
			)
		}
	}

	if logWriter != nil {
		_, _ = fmt.Fprintf(logWriter, "Successfully loaded br_netfilter kernel module\n")
	}

	return nil
}

// containsModule checks if a module name appears in /proc/modules output.
func containsModule(modulesContent, moduleName string) bool {
	// /proc/modules format: "module_name size refcount deps state offset"
	// Each module is on its own line, and the name is the first field
	for line := range strings.SplitSeq(modulesContent, "\n") {
		if len(line) == 0 {
			continue
		}
		// Get the first field (module name)
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == moduleName {
			return true
		}
	}

	return false
}
