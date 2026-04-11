//go:build windows

package workload

import (
	"context"
	"errors"
)

// ErrTalosDebugNotSupportedOnWindows is returned when attempting Talos host-level debugging on Windows.
var ErrTalosDebugNotSupportedOnWindows = errors.New(
	"talos host-level debugging requires Unix signal handling and is not supported on Windows",
)

// runTalosHostDebug is not supported on Windows due to signal handling requirements.
func runTalosHostDebug(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ []string,
) error {
	return ErrTalosDebugNotSupportedOnWindows
}
