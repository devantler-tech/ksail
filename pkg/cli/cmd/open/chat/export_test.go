package chat

import (
	"context"
	"os/exec"
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

// DiagnoseTimeout exports the diagnoseTimeout constant for testing.
const DiagnoseTimeout = diagnoseTimeout

// StartupErrFmt exports the startupErrFmt constant for testing.
const StartupErrFmt = startupErrFmt

// GetDiagnoseCLIStartupFailure returns the diagnoseCLIStartupFailure function for testing.
func GetDiagnoseCLIStartupFailure() func(context.Context, string, string, []string) string {
	return diagnoseCLIStartupFailure
}

// GetBuildDiagnosticBlock returns the buildDiagnosticBlock function for testing.
func GetBuildDiagnosticBlock() func(context.Context, string, string, []string) string {
	return buildDiagnosticBlock
}

// GetRunCopilotCmdWithRetry returns the runCopilotCmdWithRetry helper for testing.
func GetRunCopilotCmdWithRetry() func(context.Context, func() *exec.Cmd) error {
	return runCopilotCmdWithRetry
}

// GetVerifyCopilotCLI returns the verifyCopilotCLI function for testing.
func GetVerifyCopilotCLI() func(context.Context, string, []string) error {
	return verifyCopilotCLI
}

// GetRunCopilotAuthLogin returns the runCopilotAuthLogin function for testing.
func GetRunCopilotAuthLogin() func(context.Context, string) error {
	return runCopilotAuthLogin
}

// CopilotExecMaxRetries exports copilotExecMaxRetries for testing.
const CopilotExecMaxRetries = copilotExecMaxRetries

// CopilotExecRetryBackoff exports copilotExecRetryBackoff for testing.
const CopilotExecRetryBackoff = copilotExecRetryBackoff

// FormatDiagnosticOutput exposes the formatDiagnosticOutput formatting helper for
// deterministic unit tests that verify block layout without subprocess execution.
func FormatDiagnosticOutput(d string) string {
	return formatDiagnosticOutput(d)
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
