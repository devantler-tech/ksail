package chat

import (
	"context"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

// GetLoadChatConfig returns the loadChatConfig function for testing purposes.
func GetLoadChatConfig() func() chatConfig {
	return loadChatConfig
}

// GetResolveModel returns the resolveModel function for testing.
func GetResolveModel() func(string, string) string {
	return resolveModel
}

// GetValidateReasoningEffort returns the validateReasoningEffort function for testing.
func GetValidateReasoningEffort() func(string) error {
	return validateReasoningEffort
}

// GetResolveReasoningEffort returns the resolveReasoningEffort function for testing.
func GetResolveReasoningEffort() func(string, string) (string, error) {
	return resolveReasoningEffort
}

// GetFilterEnvVars returns the filterEnvVars function for testing.
func GetFilterEnvVars() func([]string, []string) []string {
	return filterEnvVars
}

// GetFilterEnvVarPrefixes returns the filterEnvVarPrefixes function for testing.
func GetFilterEnvVarPrefixes() func([]string, []string) []string {
	return filterEnvVarPrefixes
}

// AuthMaxAttempts exports the authMaxAttempts constant for testing.
const AuthMaxAttempts = authMaxAttempts

// AuthStatusChecker is the exported alias for the authStatusChecker interface.
type AuthStatusChecker = authStatusChecker

// GetAuthStatusWithRetry exposes getAuthStatusWithRetry for white-box testing.
func GetAuthStatusWithRetry(
	ctx context.Context,
	checker AuthStatusChecker,
) (*copilot.GetAuthStatusResponse, error) {
	return getAuthStatusWithRetry(ctx, checker)
}

// GetAuthStatusWithRetryOpts exposes getAuthStatusWithRetryOpts for white-box testing
// with injectable backoff durations so tests can avoid real sleep delays.
func GetAuthStatusWithRetryOpts(
	ctx context.Context,
	checker AuthStatusChecker,
	baseWait, maxWait time.Duration,
) (*copilot.GetAuthStatusResponse, error) {
	return getAuthStatusWithRetryOpts(ctx, checker, baseWait, maxWait)
}
