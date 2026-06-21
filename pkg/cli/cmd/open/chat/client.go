package chat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/netretry"
	copilot "github.com/github/copilot-sdk/go"
)

// Copilot client bootstrap constants.
const (
	// authMaxAttempts is the maximum number of attempts for auth status checks.
	authMaxAttempts = 3
	// authRetryBaseWait is the base wait duration for auth retry backoff.
	authRetryBaseWait = 500 * time.Millisecond
	// sessionIdleTimeoutSeconds is the server-side session idle timeout.
	// Sessions without activity for this duration are automatically cleaned up.
	sessionIdleTimeoutSeconds = 1800 // 30 minutes
	// authRetryMaxWait is the maximum wait duration for auth retry backoff.
	authRetryMaxWait = 4 * time.Second
	// diagnoseTimeout is the timeout for the post-failure diagnostic subprocess.
	// Kept intentionally short: ksail open chat is interactive, so a brief wait on
	// failure to surface the real root cause is an acceptable trade-off.
	diagnoseTimeout = 3 * time.Second
	// copilotExecMaxRetries bounds re-attempts when launching the Copilot CLI
	// races a concurrent fork. The SDK writes the CLI into its cache directory
	// at runtime, so a fork that inherited a still-open write fd can make the
	// kernel report the freshly written binary as "text file busy" (ETXTBSY).
	copilotExecMaxRetries = 5
	// copilotExecRetryBackoff is the pause between ETXTBSY re-attempts; the
	// write fd is closed almost immediately, so a short wait clears the race.
	copilotExecRetryBackoff = 10 * time.Millisecond
	// startupErrFmt is the static format string for Copilot client startup errors.
	// %w is the underlying error; %s is an optional diagnostic block (empty or
	// "CLI diagnostic output:\n  ...\n\n").
	startupErrFmt = "failed to start Copilot client: %w\n\n%sTo fix:\n" +
		"  - Set KSAIL_COPILOT_TOKEN or COPILOT_TOKEN for token-based authentication\n" +
		"  - Install the Copilot CLI: npm install -g @github/copilot\n" +
		"  - Verify the CLI works: copilot --version"
)

// Sentinel errors for the Copilot client bootstrap.
var errNotAuthenticated = errors.New("not authenticated with GitHub Copilot")

// setupCopilotClient starts the Copilot client, validates authentication,
// and returns a cleanup function that should be deferred.
func setupCopilotClient(
	ctx context.Context,
) (*copilot.Client, string, func(), error) {
	client, err := startCopilotClient(ctx)
	if err != nil {
		return nil, "", nil, err
	}

	cleanup := func() {
		select {
		case <-ctx.Done():
			os.Exit(signalExitCode)
		default:
			_ = client.Stop()
		}
	}

	loginName, err := validateCopilotAuth(ctx, client)
	if err != nil {
		cleanup()

		return nil, "", nil, err
	}

	return client, loginName, cleanup, nil
}

// startCopilotClient creates and starts a Copilot client.
// Authentication precedence:
//  1. KSAIL_COPILOT_TOKEN / COPILOT_TOKEN — explicit Copilot token
//  2. Copilot CLI's own OAuth credentials (device flow via `copilot auth login`)
//
// GITHUB_TOKEN is intentionally NOT used: it is a general-purpose PAT that
// may lack Copilot-specific scopes, causing API endpoints like models.list
// to return 400.
func startCopilotClient(ctx context.Context) (*copilot.Client, error) {
	// Environment variables to check for explicit Copilot tokens (priority order).
	tokenEnvVars := []string{"KSAIL_COPILOT_TOKEN", "COPILOT_TOKEN"}

	// Environment variables filtered from the child copilot CLI process to
	// prevent implicit auth interference. GITHUB_TOKEN and GH_TOKEN are
	// general-purpose PATs that may not carry Copilot-specific scopes.
	// COPILOT_GITHUB_TOKEN is the Copilot CLI's own auth token env var;
	// if set by a parent process (e.g. a GitHub App) with a token that
	// lacks Copilot scopes, it causes immediate startup failure. The SDK
	// manages auth separately via COPILOT_SDK_AUTH_TOKEN.
	//
	// NOTE: We intentionally use a specific denylist rather than filtering
	// all COPILOT_* prefixed vars, so that user-configurable settings like
	// COPILOT_CUSTOM_INSTRUCTIONS_DIRS are preserved.
	filteredEnvVars := []string{"GITHUB_TOKEN", "GH_TOKEN", "COPILOT_GITHUB_TOKEN"}

	opts := &copilot.ClientOptions{
		LogLevel:                  "error",
		Env:                       filterEnvVars(os.Environ(), filteredEnvVars),
		SessionIdleTimeoutSeconds: sessionIdleTimeoutSeconds,
	}

	// Resolve CLI path explicitly so we get a clear error if it's missing,
	// rather than the opaque "CLI process exited: exit status 1" from the SDK.
	// This checks COPILOT_CLI_PATH, the SDK cache directory, and system PATH.
	//
	// When COPILOT_CLI_PATH is explicitly set, errors are fatal: the user
	// expects that specific binary to be used. Otherwise, resolution failures
	// are ignored so the SDK can try its own resolution (embedded CLI → PATH
	// fallback) for forward compatibility.
	cliPath, pathErr := resolveCopilotCLIPath()
	if pathErr != nil && os.Getenv("COPILOT_CLI_PATH") != "" {
		return nil, pathErr
	}

	if pathErr == nil {
		verifyErr := verifyCopilotCLI(ctx, cliPath, opts.Env)
		if verifyErr != nil {
			return nil, verifyErr
		}

		opts.Connection = &copilot.StdioConnection{Path: cliPath}
	}

	cwd, cwdErr := os.Getwd()
	if cwdErr == nil {
		opts.WorkingDirectory = cwd
	}

	for _, envVar := range tokenEnvVars {
		if token := os.Getenv(envVar); token != "" {
			opts.GitHubToken = token

			break
		}
	}

	client := copilot.NewClient(opts)

	err := client.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			startupErrFmt,
			err,
			buildDiagnosticBlock(ctx, cliPath, opts.GitHubToken, opts.Env),
		)
	}

	return client, nil
}

// verifyCopilotCLI runs a quick version check on the resolved copilot binary
// to catch common issues (missing binary, corrupt install, wrong binary)
// before the SDK attempts a full startup. The provided env is used so the
// pre-flight check matches the filtered environment the SDK will use.
func verifyCopilotCLI(ctx context.Context, cliPath string, env []string) error {
	const verifyTimeout = 5 * time.Second

	verifyCtx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	var output bytes.Buffer

	err := runCopilotCmdWithRetry(verifyCtx, func() *exec.Cmd {
		output.Reset()

		cmd := exec.CommandContext(verifyCtx, cliPath, "--version")
		cmd.Env = env
		cmd.Stdout = &output
		cmd.Stderr = &output

		return cmd
	})
	if err != nil {
		return fmt.Errorf(
			"copilot CLI at %q failed pre-flight check: %w (output: %s)\n\n"+
				"To fix:\n"+
				"  - Install or reinstall the Copilot CLI: npm install -g @github/copilot\n"+
				"  - Or set COPILOT_CLI_PATH to a working copilot binary",
			cliPath, err, strings.TrimSpace(output.String()),
		)
	}

	return nil
}

// formatDiagnosticOutput wraps a non-empty diagnostic string in an indented
// display block ready for inclusion in an error message.
func formatDiagnosticOutput(d string) string {
	return "CLI diagnostic output:\n  " + strings.ReplaceAll(d, "\n", "\n  ") + "\n\n"
}

// buildDiagnosticBlock runs the CLI diagnostic and returns a formatted block
// suitable for inclusion in an error message, or an empty string if there is
// nothing to report. When githubToken is non-empty, it is injected into the
// subprocess environment via COPILOT_SDK_AUTH_TOKEN (mirroring the SDK) so the
// diagnostic runs under the same auth context as the real startup attempt.
func buildDiagnosticBlock(ctx context.Context, cliPath, githubToken string, env []string) string {
	if cliPath == "" {
		return ""
	}

	d := diagnoseCLIStartupFailure(ctx, cliPath, githubToken, env)
	if d == "" {
		return ""
	}

	return formatDiagnosticOutput(d)
}

// diagnoseCLIStartupFailure re-runs the copilot CLI in headless mode with
// stderr captured, returning any diagnostic output. The Copilot SDK discards
// the subprocess's stderr, so this post-failure re-run is the only way to
// surface why the CLI crashed during startup.
//
// When githubToken is non-empty, it is passed via the same COPILOT_SDK_AUTH_TOKEN
// env var and --auth-token-env flag that the SDK uses, so the diagnostic
// reproduces the same auth path as the real startup.
func diagnoseCLIStartupFailure(
	ctx context.Context,
	cliPath, githubToken string,
	env []string,
) string {
	diagCtx, cancel := context.WithTimeout(ctx, diagnoseTimeout)
	defer cancel()

	args := []string{"--headless", "--no-auto-update", "--log-level", "error", "--stdio"}
	diagEnv := env

	if githubToken != "" {
		args = append(args, "--auth-token-env", "COPILOT_SDK_AUTH_TOKEN")
		diagEnv = append(append([]string{}, diagEnv...), "COPILOT_SDK_AUTH_TOKEN="+githubToken)
	}

	var stderr bytes.Buffer

	_ = runCopilotCmdWithRetry(diagCtx, func() *exec.Cmd {
		stderr.Reset()

		cmd := exec.CommandContext(diagCtx, cliPath, args...)
		cmd.Env = diagEnv
		cmd.Stderr = &stderr

		return cmd
	})

	return strings.TrimSpace(stderr.String())
}

// runCopilotCmdWithRetry runs the command produced by newCmd, rebuilding and
// retrying it when launching the binary fails with ETXTBSY. The Copilot SDK
// writes its CLI into a cache directory at runtime, so exec can race a
// concurrent fork that inherited a still-open write fd and have the kernel
// report the freshly written binary as "text file busy". ETXTBSY surfaces at
// execve before any I/O, so re-launching is safe; an exec.Cmd is single-use, so
// each attempt builds a fresh one via newCmd. ctx bounds the total retry window.
func runCopilotCmdWithRetry(ctx context.Context, newCmd func() *exec.Cmd) error {
	for attempt := 0; ; attempt++ {
		err := newCmd().Run()
		if errors.Is(err, syscall.ETXTBSY) && attempt < copilotExecMaxRetries {
			// Interruptible backoff: a cancelled or expired context aborts the
			// loop and surfaces the cancellation reason rather than waiting out
			// the timer and reporting the transient ETXTBSY.
			timer := time.NewTimer(copilotExecRetryBackoff)
			select {
			case <-timer.C:
				continue
			case <-ctx.Done():
				timer.Stop()

				return ctx.Err() //nolint:wrapcheck // propagate cancellation as-is
			}
		}

		// Callers wrap (or intentionally ignore) this error; the helper is a
		// thin retry wrapper around the exec.Cmd.Run() they would call directly.
		return err //nolint:wrapcheck // callers wrap; thin exec.Cmd.Run retry pass-through
	}
}

// filterEnvVars returns a copy of env with the specified variable names removed.
// Comparison is case-sensitive (matching os.Environ() format "KEY=value").
func filterEnvVars(env []string, remove []string) []string {
	filtered := make([]string, 0, len(env))

	for _, entry := range env {
		exclude := false

		for _, name := range remove {
			prefix := name + "="
			if len(entry) >= len(prefix) && entry[:len(prefix)] == prefix {
				exclude = true

				break
			}
		}

		if !exclude {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

// authStatusChecker is the minimal interface required by getAuthStatusWithRetry,
// allowing the concrete *copilot.Client to be swapped for a test double.
type authStatusChecker interface {
	GetAuthStatus(ctx context.Context) (*copilot.GetAuthStatusResponse, error)
}

// validateCopilotAuth checks authentication. If not authenticated, it attempts
// an inline `copilot auth login` device flow before returning an error.
func validateCopilotAuth(ctx context.Context, client *copilot.Client) (string, error) {
	authStatus, err := getAuthStatusWithRetry(ctx, client)
	if err != nil {
		return "", fmt.Errorf("failed to check authentication: %w", err)
	}

	if !authStatus.IsAuthenticated {
		authStatus, err = attemptInlineLogin(ctx, client)
		if err != nil {
			return "", err
		}
	}

	loginName := "unknown"
	if authStatus.Login != nil {
		loginName = *authStatus.Login
	}

	return loginName, nil
}

// attemptInlineLogin runs an interactive `copilot auth login` device flow
// and returns the updated auth status on success.
func attemptInlineLogin(
	ctx context.Context,
	client *copilot.Client,
) (*copilot.GetAuthStatusResponse, error) {
	cliPath, pathErr := resolveCopilotCLIPath()
	if pathErr != nil {
		return nil, fmt.Errorf(
			"%w\n\n"+
				"could not find the Copilot CLI to start login flow: %v\n\n"+
				"To fix:\n"+
				"  - Set KSAIL_COPILOT_TOKEN or COPILOT_TOKEN for token-based authentication\n"+
				"  - Ensure you have an active GitHub Copilot subscription",
			errNotAuthenticated, pathErr,
		)
	}

	fmt.Fprintln(os.Stderr, "\nNot authenticated with GitHub Copilot. Starting login...")

	loginErr := runCopilotAuthLogin(ctx, cliPath)
	if loginErr != nil {
		return nil, fmt.Errorf("%w: login failed: %w", errNotAuthenticated, loginErr)
	}

	authStatus, err := getAuthStatusWithRetry(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to verify authentication after login: %w", err)
	}

	if !authStatus.IsAuthenticated {
		msg := "login completed but authentication check still fails"
		if authStatus.StatusMessage != nil {
			msg += ": " + *authStatus.StatusMessage
		}

		return nil, fmt.Errorf("%w: %s", errNotAuthenticated, msg)
	}

	return authStatus, nil
}

// isAuthStatusRetryable reports whether err should be retried during auth status checks.
// It augments the generic netretry.IsRetryable with a Copilot-auth-specific check:
// "fetch failed" is a transient error emitted by the Copilot subprocess when it has not
// yet fully initialized, and is not a generic network error that should be retried
// globally across all callers (Helm/Docker/OCI/etc.).
func isAuthStatusRetryable(err error) bool {
	if netretry.IsRetryable(err) {
		return true
	}

	return strings.Contains(err.Error(), "fetch failed")
}

// getAuthStatusWithRetry calls GetAuthStatus with exponential backoff retries
// for transient errors (e.g., "fetch failed" when the Copilot subprocess
// hasn't fully initialized).
func getAuthStatusWithRetry(
	ctx context.Context,
	client authStatusChecker,
) (*copilot.GetAuthStatusResponse, error) {
	return getAuthStatusWithRetryOpts(ctx, client, authRetryBaseWait, authRetryMaxWait)
}

// getAuthStatusWithRetryOpts is the underlying implementation for getAuthStatusWithRetry
// with injectable baseWait/maxWait durations to support testing without real sleep delays.
func getAuthStatusWithRetryOpts(
	ctx context.Context,
	client authStatusChecker,
	baseWait, maxWait time.Duration,
) (*copilot.GetAuthStatusResponse, error) {
	var (
		lastErr     error
		lastAttempt int
	)

	for attempt := 1; attempt <= authMaxAttempts; attempt++ {
		authStatus, err := client.GetAuthStatus(ctx)
		if err == nil {
			return authStatus, nil
		}

		lastErr = err
		lastAttempt = attempt

		if !isAuthStatusRetryable(lastErr) || attempt == authMaxAttempts {
			break
		}

		delay := netretry.ExponentialDelay(attempt, baseWait, maxWait)

		// Standard cancellable backoff sleep; coincidentally identical to other
		// netretry loops but operates on a distinct domain (auth status check).
		// jscpd:ignore-start
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}

			return nil, fmt.Errorf("auth status check cancelled: %w", ctx.Err())
		case <-timer.C:
		}
		// jscpd:ignore-end
	}

	if !isAuthStatusRetryable(lastErr) {
		return nil, fmt.Errorf(
			"auth status check failed on attempt %d/%d (non-retryable): %w",
			lastAttempt,
			authMaxAttempts,
			lastErr,
		)
	}

	return nil, fmt.Errorf(
		"auth status check failed after %d/%d attempts: %w",
		lastAttempt,
		authMaxAttempts,
		lastErr,
	)
}

// resolveCopilotCLIPath finds the Copilot CLI binary, checking:
//  1. COPILOT_CLI_PATH environment variable (validated for existence)
//  2. SDK cache directory (bundled CLI)
//  3. System PATH
func resolveCopilotCLIPath() (string, error) {
	if envPath := os.Getenv("COPILOT_CLI_PATH"); envPath != "" {
		cleanPath := filepath.Clean(envPath)

		_, err := os.Stat(cleanPath)
		if err != nil {
			return "", fmt.Errorf("COPILOT_CLI_PATH %q is not accessible: %w", cleanPath, err)
		}

		return cleanPath, nil
	}

	if p, found := findCopilotInSDKCache(); found {
		return p, nil
	}

	p, lookErr := exec.LookPath("copilot")
	if lookErr != nil {
		return "", fmt.Errorf("copilot CLI not found in PATH: %w", lookErr)
	}

	return p, nil
}

// findCopilotInSDKCache looks for the copilot executable in the SDK cache directory.
// Only matches files named exactly "copilot" or "copilot-<platform>" (e.g., "copilot-linux-amd64").
// Files like "copilot-config" or "copilot-backup" are excluded.
func findCopilotInSDKCache() (string, bool) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", false
	}

	sdkDir := filepath.Join(cacheDir, "copilot-sdk")

	entries, err := os.ReadDir(sdkDir)
	if err != nil {
		return "", false
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() ||
			!isCopilotBinaryName(name) {
			continue
		}

		info, statErr := entry.Info()
		if statErr != nil {
			continue
		}

		// Require at least one executable bit to be set.
		if info.Mode()&0o111 == 0 {
			continue
		}

		return filepath.Join(sdkDir, name), true
	}

	return "", false
}

// isCopilotBinaryName returns true for names that match the Copilot CLI binary pattern:
// "copilot", "copilot.exe", or "copilot-<segment>[-<segment>...][.exe]"
// (e.g., "copilot-linux-amd64", "copilot-linux-amd64.exe").
// Rejects empty segments (trailing dash "copilot-linux-", leading dash "copilot--amd64",
// double dashes "copilot-linux--amd64"), non-binary extensions (.json, .yaml, etc.),
// and bare prefix ("copilot-" with no segments).
func isCopilotBinaryName(name string) bool {
	if name == "copilot" || name == "copilot.exe" {
		return true
	}

	// Strip optional .exe suffix for platform-specific binaries
	// (e.g., "copilot-windows-amd64.exe").
	base := strings.TrimSuffix(name, ".exe")

	// Require a "copilot-" prefix for platform-specific binaries.
	if !strings.HasPrefix(base, "copilot-") {
		return false
	}

	// Reject known non-binary suffixes for names that otherwise
	// look like Copilot binaries.
	if hasNonBinarySuffix(name) {
		return false
	}

	rest := strings.TrimPrefix(base, "copilot-")
	if rest == "" {
		return false
	}

	// Allow one or more non-empty segments separated by dashes
	// (e.g., "linux-amd64", "linux").
	return !strings.Contains(rest, "--") &&
		!strings.HasPrefix(rest, "-") &&
		!strings.HasSuffix(rest, "-")
}

// hasNonBinarySuffix returns true if the filename has a known non-binary extension.
func hasNonBinarySuffix(name string) bool {
	nonBinarySuffixes := []string{".lock", ".license", ".json", ".yaml", ".yml", ".txt", ".log"}

	for _, suffix := range nonBinarySuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}

	return false
}

// runCopilotAuthLogin spawns `copilot auth login` as an interactive subprocess.
// cliPath is trusted user input from COPILOT_CLI_PATH or the Copilot SDK directory.
func runCopilotAuthLogin(ctx context.Context, cliPath string) error {
	err := runCopilotCmdWithRetry(ctx, func() *exec.Cmd {
		cmd := exec.CommandContext(ctx, cliPath, "auth", "login")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		return cmd
	})
	if err != nil {
		return fmt.Errorf("copilot auth login failed: %w", err)
	}

	return nil
}
