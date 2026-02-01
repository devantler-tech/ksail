package cloudproviderkindinstaller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	cpkcmd "sigs.k8s.io/cloud-provider-kind/cmd"
)

var (
	// Global state for the cloud-provider-kind controller.
	globalController *cloudProviderController //nolint:gochecknoglobals // Required for singleton controller
	globalMu         sync.Mutex               //nolint:gochecknoglobals // Required for singleton controller
)

// cloudProviderController wraps the cloud-provider-kind controller with lifecycle management.
type cloudProviderController struct {
	cancel   context.CancelFunc
	refCount int
	done     chan struct{}
}

// CloudProviderKINDInstaller manages the cloud-provider-kind controller as a background goroutine.
type CloudProviderKINDInstaller struct {
	// No fields needed - controller is managed globally
}

// NewCloudProviderKINDInstaller creates a new Cloud Provider KIND installer instance.
func NewCloudProviderKINDInstaller() *CloudProviderKINDInstaller {
	return &CloudProviderKINDInstaller{}
}

// Install starts the cloud-provider-kind controller if not already running.
// The controller runs as a background goroutine and monitors all KIND clusters.
// Multiple calls to Install() will increment a reference count, ensuring the
// controller stays running as long as at least one cluster needs it.
//
//nolint:contextcheck // Background context is intentional for long-running controller
func (c *CloudProviderKINDInstaller) Install(_ context.Context) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	// Check if another ksail process is managing cloud-provider-kind
	if isRunningExternally() {
		// Another process is managing it, just increment local reference
		if globalController == nil {
			// Initialize local state but don't start the controller
			globalController = &cloudProviderController{
				refCount: 1,
			}
		} else {
			globalController.refCount++
		}

		return nil
	}

	// If controller is already running in this process, increment reference count
	if globalController != nil && globalController.done != nil {
		globalController.refCount++

		return nil
	}

	// Start the cloud-provider-kind controller using the cmd package
	cmd := cpkcmd.NewCommand()

	// Create a cancelable context for the controller
	// Background context is intentional for long-running controller
	ctrlCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	// Start controller in background goroutine
	go func() {
		defer close(done)
		// Run the command - this will block until context is canceled
		_ = cmd.ExecuteContext(ctrlCtx)
	}()

	// Mark as running and create lock file
	err := createLockFile()
	if err != nil {
		cancel()
		<-done // Wait for goroutine to finish

		return fmt.Errorf("failed to create lock file: %w", err)
	}

	globalController = &cloudProviderController{
		cancel:   cancel,
		refCount: 1,
		done:     done,
	}

	return nil
}

// Uninstall decrements the reference count and stops the cloud-provider-kind controller
// if no more clusters are using it.
func (c *CloudProviderKINDInstaller) Uninstall(_ context.Context) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalController == nil {
		return nil // Nothing to uninstall
	}

	globalController.refCount--

	// Only stop the controller if reference count reaches zero
	if globalController.refCount <= 0 {
		if globalController.cancel != nil {
			globalController.cancel()

			// Wait for the goroutine to finish
			if globalController.done != nil {
				<-globalController.done
			}
		}

		// Remove lock file
		err := removeLockFile()
		if err != nil {
			// Log error but don't fail uninstall
			fmt.Fprintf(os.Stderr, "Warning: failed to remove lock file: %v\n", err)
		}

		globalController = nil
	}

	return nil
}

// --- Lock file management ---

const lockFileName = "cloud-provider-kind.lock"

func getLockFilePath() string {
	// Use a temporary directory for the lock file
	tmpDir := os.TempDir()

	return filepath.Join(tmpDir, lockFileName)
}

func isRunningExternally() bool {
	lockPath := getLockFilePath()

	_, statErr := os.Stat(lockPath)
	if statErr == nil {
		// Lock file exists - check if the process is still running
		// For now, we assume if the file exists, it's running
		// In production, we'd want to validate the PID
		return true
	}

	return false
}

func createLockFile() error {
	lockPath := getLockFilePath()

	// Create lock file with current process ID
	pid := os.Getpid()
	content := fmt.Sprintf("%d\n", pid)

	const lockFilePermissions = 0o600

	err := os.WriteFile(lockPath, []byte(content), lockFilePermissions)
	if err != nil {
		return fmt.Errorf("failed to write lock file: %w", err)
	}

	return nil
}

func removeLockFile() error {
	lockPath := getLockFilePath()

	err := os.Remove(lockPath)
	if err != nil {
		return fmt.Errorf("failed to remove lock file: %w", err)
	}

	return nil
}
