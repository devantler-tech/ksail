package docker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// containerStatusChecker is a function type for checking if a container is running.
// Different implementations are used for KSail-labeled vs unlabeled containers.
type containerStatusChecker func(ctx context.Context, name string) (bool, error)

// WaitForRegistryReady waits for a registry to become ready by polling its health endpoint.
// It checks the registry's host port (mapped to localhost) to verify the registry is responding.
// The containerIP parameter is currently unused but kept for API compatibility and future use.
func (rm *RegistryManager) WaitForRegistryReady(
	ctx context.Context,
	name string,
	_ string, // containerIP - unused, we always check via host port
) error {
	return rm.WaitForRegistryReadyWithTimeout(ctx, name, RegistryReadyTimeout)
}

// WaitForRegistryReadyWithTimeout waits for a registry with a custom timeout.
// For mirror registries without host port bindings, this only verifies the container is running.
func (rm *RegistryManager) WaitForRegistryReadyWithTimeout(
	ctx context.Context,
	name string,
	timeout time.Duration,
) error {
	checkURL, err := rm.prepareHealthCheck(ctx, name)
	if err != nil {
		// If the registry has no host port (mirror registry), just verify it's running
		if errors.Is(err, ErrRegistryPortNotFound) {
			inUse, statusErr := rm.IsRegistryInUse(ctx, name)
			if statusErr != nil {
				return fmt.Errorf("failed to check if registry %s is running: %w", name, statusErr)
			}

			if !inUse {
				return fmt.Errorf("registry %s is not running: %w", name, ErrRegistryNotFound)
			}

			// Mirror registry is running - no further health check needed
			return nil
		}

		return err
	}

	return rm.pollUntilReady(ctx, name, checkURL, timeout, rm.IsRegistryInUse)
}

// WaitForRegistriesReady waits for multiple registries to become ready.
// The registryIPs map contains registry names as keys (IP values are ignored).
func (rm *RegistryManager) WaitForRegistriesReady(
	ctx context.Context,
	registryIPs map[string]string,
) error {
	return rm.WaitForRegistriesReadyWithTimeout(ctx, registryIPs, RegistryReadyTimeout)
}

// WaitForRegistriesReadyWithTimeout waits for multiple registries with a custom timeout.
func (rm *RegistryManager) WaitForRegistriesReadyWithTimeout(
	ctx context.Context,
	registryIPs map[string]string,
	timeout time.Duration,
) error {
	for name := range registryIPs {
		err := rm.WaitForRegistryReadyWithTimeout(ctx, name, timeout)
		if err != nil {
			return fmt.Errorf("registry %s failed health check: %w", name, err)
		}
	}

	return nil
}

// WaitForContainerRegistryReady waits for a registry container to become ready without
// requiring KSail labels. This is designed for K3d-managed registries that are created
// by K3d rather than KSail.
func (rm *RegistryManager) WaitForContainerRegistryReady(
	ctx context.Context,
	name string,
	timeout time.Duration,
) error {
	checkURL, err := rm.prepareContainerHealthCheck(ctx, name)
	if err != nil {
		return err
	}

	return rm.pollUntilReady(ctx, name, checkURL, timeout, rm.IsContainerRunning)
}

// prepareContainerHealthCheck validates the container and returns the health check URL.
// Unlike prepareHealthCheck, this method does not require KSail labels.
func (rm *RegistryManager) prepareContainerHealthCheck(
	ctx context.Context,
	name string,
) (string, error) {
	running, err := rm.IsContainerRunning(ctx, name)
	if err != nil {
		return "", fmt.Errorf("failed to check if container %s is running: %w", name, err)
	}

	if !running {
		return "", fmt.Errorf("container %s is not running: %w", name, ErrRegistryNotFound)
	}

	port, portErr := rm.GetContainerPort(ctx, name, DefaultRegistryPort)
	if portErr != nil {
		return "", fmt.Errorf("failed to get container port: %w", portErr)
	}

	return buildHealthCheckURL(port), nil
}

// prepareHealthCheck validates the registry and returns the health check URL.
func (rm *RegistryManager) prepareHealthCheck(
	ctx context.Context,
	name string,
) (string, error) {
	inUse, err := rm.IsRegistryInUse(ctx, name)
	if err != nil {
		return "", fmt.Errorf("failed to check if registry %s is running: %w", name, err)
	}

	if !inUse {
		return "", fmt.Errorf("registry %s is not running: %w", name, ErrRegistryNotFound)
	}

	port, portErr := rm.GetRegistryPort(ctx, name)
	if portErr != nil {
		return "", fmt.Errorf("failed to get registry port: %w", portErr)
	}

	return buildHealthCheckURL(port), nil
}

// buildHealthCheckURL constructs the health check URL for a registry on the given port.
func buildHealthCheckURL(port int) string {
	checkAddr := net.JoinHostPort(RegistryHostIP, strconv.Itoa(port))

	return fmt.Sprintf("http://%s/v2/", checkAddr)
}

// pollUntilReady polls the registry health endpoint until it responds or timeout.
// The statusChecker is used to verify if the container is still running after errors.
func (rm *RegistryManager) pollUntilReady(
	ctx context.Context,
	name string,
	checkURL string,
	timeout time.Duration,
	statusChecker containerStatusChecker,
) error {
	httpClient := &http.Client{Timeout: RegistryHTTPTimeout}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(RegistryReadyPollInterval)

	defer ticker.Stop()

	state := &pollState{}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %w", ErrRegistryHealthCheckCancelled, ctx.Err())
		case <-ticker.C:
			if time.Now().After(deadline) {
				return rm.buildTimeoutError(name, state.lastErr)
			}

			done, err := rm.pollOnce(ctx, name, checkURL, httpClient, state, statusChecker)
			if err != nil {
				return err
			}

			if done {
				return nil
			}
		}
	}
}

// pollState tracks state across poll iterations.
type pollState struct {
	lastErr                error
	connectionRefusedCount int
}

// pollOnce performs a single poll iteration.
// Returns (true, nil) if ready, (false, nil) to continue polling, or (false, err) to stop with error.
func (rm *RegistryManager) pollOnce(
	ctx context.Context,
	name, checkURL string,
	httpClient *http.Client,
	state *pollState,
	statusChecker containerStatusChecker,
) (bool, error) {
	ready, err := rm.checkRegistryHealth(ctx, httpClient, checkURL)
	if err == nil && ready {
		return true, nil
	}

	if err != nil {
		state.lastErr = err

		crashErr := rm.handleConnectionRefused(ctx, name, err, state, statusChecker)
		if crashErr != nil {
			return false, crashErr
		}
	}

	return false, nil
}

// handleConnectionRefused checks if the container crashed after repeated connection refused errors.
// Returns an error if the container is not running, nil otherwise.
func (rm *RegistryManager) handleConnectionRefused(
	ctx context.Context,
	name string,
	err error,
	state *pollState,
	statusChecker containerStatusChecker,
) error {
	if !isConnectionRefused(err) {
		state.connectionRefusedCount = 0

		return nil
	}

	state.connectionRefusedCount++

	if state.connectionRefusedCount < ConnectionRefusedCheckThreshold {
		return nil
	}

	// Check container state after several consecutive connection refused errors
	running, checkErr := statusChecker(ctx, name)
	if checkErr == nil && !running {
		return fmt.Errorf(
			"%w: %s (container is not running)",
			ErrRegistryNotReady,
			name,
		)
	}

	// Reset counter to avoid checking too frequently
	state.connectionRefusedCount = 0

	return nil
}

// isConnectionRefused checks if the error indicates a connection refused.
func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connect: connection refused")
}

// checkRegistryHealth performs a single health check request.
// Returns (true, nil) if ready, (false, error) if not ready yet.
func (rm *RegistryManager) checkRegistryHealth(
	ctx context.Context,
	httpClient *http.Client,
	checkURL string,
) (bool, error) {
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if reqErr != nil {
		return false, fmt.Errorf("failed to create health check request: %w", reqErr)
	}

	resp, respErr := httpClient.Do(req)
	if respErr != nil {
		return false, fmt.Errorf("health check request failed: %w", respErr)
	}

	_ = resp.Body.Close()

	// Registry v2 API returns 200 or 401 (if auth required) on /v2/
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
		return true, nil
	}

	return false, fmt.Errorf("%w: %d", ErrRegistryUnexpectedStatus, resp.StatusCode)
}

// buildTimeoutError creates the appropriate timeout error with optional last error context.
func (rm *RegistryManager) buildTimeoutError(name string, lastErr error) error {
	if lastErr != nil {
		return fmt.Errorf("%w: %s (last error: %w)", ErrRegistryNotReady, name, lastErr)
	}

	return fmt.Errorf("%w: %s", ErrRegistryNotReady, name)
}
