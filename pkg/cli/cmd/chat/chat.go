package chat

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	chatui "github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	"github.com/devantler-tech/ksail/v7/pkg/client/netretry"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	chatsvc "github.com/devantler-tech/ksail/v7/pkg/svc/chat"
	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

// Chat command constants.
const (
	// signalExitCode is the standard exit code for Ctrl+C / SIGINT.
	signalExitCode = 130
	// eventChannelBuffer is the buffer size for TUI event channels.
	eventChannelBuffer = 100
	// outputChannelBuffer is the buffer size for tool output streaming channels.
	outputChannelBuffer = 100
	// defaultTimeoutMinutes is the default response timeout in minutes.
	defaultTimeoutMinutes = 5
	// signalSleepDuration is the delay before exiting after a signal to allow cleanup.
	signalSleepDuration = 50 * time.Millisecond

	// authMaxAttempts is the maximum number of attempts for auth status checks.
	authMaxAttempts = 3
	// authRetryBaseWait is the base wait duration for auth retry backoff.
	authRetryBaseWait = 500 * time.Millisecond
	// authRetryMaxWait is the maximum wait duration for auth retry backoff.
	authRetryMaxWait = 4 * time.Second
)

// Sentinel errors for the chat command.
var (
	errNotAuthenticated       = errors.New("not authenticated with GitHub Copilot")
	errResponseTimeout        = errors.New("response timeout")
	errSessionError           = errors.New("session error")
	errInvalidReasoningEffort = errors.New("invalid reasoning effort: must be low, medium, or high")
)

// flags holds parsed flags for the chat command.
type flags struct {
	model           string
	reasoningEffort string
	streaming       bool
	timeout         time.Duration
	useTUI          bool
}

// parseChatFlags extracts and resolves chat command flags.
func parseChatFlags(cmd *cobra.Command) (flags, error) {
	modelFlag, _ := cmd.Flags().GetString("model")
	reasoningEffortFlag, _ := cmd.Flags().GetString("reasoning-effort")

	// Validate reasoning effort if provided via flag
	err := validateReasoningEffort(reasoningEffortFlag)
	if err != nil {
		return flags{}, err
	}

	streaming, _ := cmd.Flags().GetBool("streaming")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	useTUI, _ := cmd.Flags().GetBool("tui")

	// Load config values
	cfg := loadChatConfig()

	// Determine model: flag > config > "" (auto)
	model := resolveModel(modelFlag, cfg.Model)

	// Determine reasoning effort: flag > config > ""
	reasoningEffort, err := resolveReasoningEffort(reasoningEffortFlag, cfg.ReasoningEffort)
	if err != nil {
		return flags{}, err
	}

	return flags{
		model:           model,
		reasoningEffort: reasoningEffort,
		streaming:       streaming,
		timeout:         timeout,
		useTUI:          useTUI,
	}, nil
}

func validateReasoningEffort(effort string) error {
	if effort == "" {
		return nil
	}

	switch effort {
	case "low", "medium", "high":
		return nil
	default:
		return fmt.Errorf("%w: %q", errInvalidReasoningEffort, effort)
	}
}

func resolveModel(flagValue, configValue string) string {
	if flagValue != "" {
		return flagValue
	}

	if configValue != "" && configValue != "auto" {
		return configValue
	}

	return ""
}

func resolveReasoningEffort(flagValue, configValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}

	if configValue != "" {
		err := validateReasoningEffort(configValue)
		if err != nil {
			return "", fmt.Errorf("%w: %q (from config)", errInvalidReasoningEffort, configValue)
		}

		return configValue, nil
	}

	return "", nil
}

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
		LogLevel: "error",
		Env:      filterEnvVars(os.Environ(), filteredEnvVars),
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

		opts.CLIPath = cliPath
	}

	cwd, cwdErr := os.Getwd()
	if cwdErr == nil {
		opts.Cwd = cwd
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
			"failed to start Copilot client: %w\n\n"+
				"To fix:\n"+
				"  - Set KSAIL_COPILOT_TOKEN or COPILOT_TOKEN for token-based authentication\n"+
				"  - Install the Copilot CLI: npm install -g @github/copilot\n"+
				"  - Verify the CLI works: copilot --version",
			err,
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

	cmd := exec.CommandContext(verifyCtx, cliPath, "--version")
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"copilot CLI at %q failed pre-flight check: %w (output: %s)\n\n"+
				"To fix:\n"+
				"  - Install or reinstall the Copilot CLI: npm install -g @github/copilot\n"+
				"  - Or set COPILOT_CLI_PATH to a working copilot binary",
			cliPath, err, strings.TrimSpace(string(output)),
		)
	}

	return nil
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

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}

			return nil, fmt.Errorf("auth status check cancelled: %w", ctx.Err())
		case <-timer.C:
		}
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
	cmd := exec.CommandContext(ctx, cliPath, "auth", "login")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("copilot auth login failed: %w", err)
	}

	return nil
}

// buildSessionConfig creates the Copilot session configuration.
func buildSessionConfig(
	model string,
	reasoningEffort string,
	streaming bool,
	sections map[string]copilot.SectionOverride,
) *copilot.SessionConfig {
	backgroundThreshold := 0.80
	exhaustionThreshold := 0.95

	config := &copilot.SessionConfig{
		Streaming: streaming,
		SystemMessage: &copilot.SystemMessageConfig{
			Mode:     "customize",
			Sections: sections,
		},
		InfiniteSessions: &copilot.InfiniteSessionConfig{
			Enabled:                       new(true),
			BackgroundCompactionThreshold: &backgroundThreshold,
			BufferExhaustionThreshold:     &exhaustionThreshold,
		},
	}

	if model != "" {
		config.Model = model
	}

	if reasoningEffort != "" {
		config.ReasoningEffort = reasoningEffort
	}

	return config
}

// NewChatCmd creates and returns the chat command.
func NewChatCmd(_ *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Start an AI-assisted chat session",
		Long: `Start an interactive AI chat session powered by GitHub Copilot.

The assistant understands KSail's CLI, configuration schemas, and can help with:
  - Guided cluster configuration and setup
  - Troubleshooting cluster issues
  - Explaining KSail concepts and features
  - Running KSail commands with your approval

Prerequisites:
  - An active GitHub Copilot subscription

Write operations require explicit confirmation before execution.`,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationExclude: "true",
		},
	}

	// Optional flags
	cmd.Flags().StringP("model", "m", "", "Model to use (e.g., gpt-5, claude-sonnet-4)")
	cmd.Flags().StringP(
		"reasoning-effort", "r", "",
		"Reasoning effort level for models that support it (low, medium, high)",
	)
	cmd.Flags().BoolP("streaming", "s", true, "Enable streaming responses")
	cmd.Flags().DurationP(
		"timeout", "t", defaultTimeoutMinutes*time.Minute,
		"Response timeout duration",
	)
	cmd.Flags().Bool("tui", true, "Use interactive TUI mode with markdown rendering")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return handleChatRunE(cmd)
	}

	return cmd
}

// notifyNonTUIStartup sends startup notifications when running outside the TUI.
func notifyNonTUIStartup(writer io.Writer) {
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Starting KSail AI Assistant...",
		Emoji:   "🤖",
		Writer:  writer,
	})
}

// handleChatRunE handles the chat command execution.
func handleChatRunE(cmd *cobra.Command) error {
	writer := cmd.OutOrStdout()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	flags, err := parseChatFlags(cmd)
	if err != nil {
		return err
	}

	if !flags.useTUI {
		setupNonTUISignalHandler(cancel, writer)
		notifyNonTUIStartup(writer)
	}

	client, loginName, cleanup, err := setupCopilotClient(ctx)
	if err != nil {
		return err
	}

	defer cleanup()

	if !flags.useTUI {
		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: "Authenticated as " + loginName,
			Writer:  writer,
		})
	}

	sections := chatsvc.BuildSystemSections()

	sessionConfig := buildSessionConfig(
		flags.model,
		flags.reasoningEffort,
		flags.streaming,
		sections,
	)

	if flags.useTUI {
		return runTUIChat(ctx, client, sessionConfig, flags.timeout, cmd.Root())
	}

	return runNonTUIChat(ctx, client, sessionConfig, flags, cmd, writer)
}

// chatConfig holds configuration values loaded from ksail.yaml.
type chatConfig struct {
	Model           string
	ReasoningEffort string
}

// loadChatConfig loads chat configuration from ksail.yaml.
// Returns empty strings if config doesn't exist or values are not set.
func loadChatConfig() chatConfig {
	// Try to load ksail.yaml from current directory
	configPath := "ksail.yaml"

	data, err := os.ReadFile(configPath)
	if err != nil {
		// Config doesn't exist or can't be read - use defaults
		return chatConfig{}
	}

	var config v1alpha1.Cluster

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		// Config exists but couldn't be parsed - ignore and use defaults
		return chatConfig{}
	}

	return chatConfig{
		Model:           config.Spec.Chat.Model,
		ReasoningEffort: config.Spec.Chat.ReasoningEffort,
	}
}

// setupNonTUISignalHandler configures signal handling for non-TUI mode.
func setupNonTUISignalHandler(
	cancel context.CancelFunc,
	writer io.Writer,
) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: "\nReceived interrupt signal, shutting down...",
			Writer:  writer,
		})
		cancel()
		time.Sleep(signalSleepDuration)
		os.Exit(signalExitCode)
	}()
}

// runNonTUIChat handles the non-TUI chat mode.
func runNonTUIChat(
	ctx context.Context,
	client *copilot.Client,
	sessionConfig *copilot.SessionConfig,
	flags flags,
	cmd *cobra.Command,
	writer io.Writer,
) error {
	// Create session log ref for SDK-native tool logging
	sessionLog := toolgen.NewSessionLogRef()

	// Set up tools without streaming
	tools, toolMetadata := chatsvc.GetKSailToolMetadata( //nolint:contextcheck
		cmd.Root(), nil, sessionLog,
	)
	tools = WrapToolsWithForceInjection(tools, toolMetadata)
	sessionConfig.Tools = tools

	// Set up permission handler for non-KSail tools (git, shell, etc.)
	sessionConfig.OnPermissionRequest = chatsvc.CreatePermissionHandler(writer)

	// Set up pre-tool-use hook for path sandboxing
	allowedRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to determine working directory for sandboxing: %w", err)
	}

	sessionConfig.Hooks = &copilot.SessionHooks{
		OnPreToolUse: BuildPreToolUseHook(allowedRoot),
	}

	// Register OnEvent handler for session-level events not in the per-turn handler
	sessionConfig.OnEvent = buildNonTUIOnEventHandler(writer)

	// Register slash commands for non-TUI mode
	sessionConfig.Commands = chatui.BuildNonTUISlashCommands(writer)

	// Create shared stdin reader for both the interactive loop and elicitation handler.
	// Using a single bufio.Reader prevents data loss from buffered reads.
	stdinReader := bufio.NewReader(os.Stdin)

	// Register elicitation handler for MCP tool form requests
	sessionConfig.OnElicitationRequest = chatsvc.CreateElicitationHandler(stdinReader, writer)

	// Create session
	session, err := client.CreateSession(ctx, sessionConfig)
	if err != nil {
		return fmt.Errorf("failed to create chat session: %w", err)
	}

	// Wire session log now that the session exists
	wireSessionLog(session, sessionLog)

	defer func() {
		select {
		case <-ctx.Done():
			os.Exit(signalExitCode)
		default:
			_ = session.Disconnect()
		}
	}()

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "Chat session started. Type 'exit' or 'quit' to end the session.",
		Writer:  writer,
	})
	_, _ = fmt.Fprintln(writer, "")

	return runChatInteractiveLoop(ctx, session, flags.streaming, flags.timeout, stdinReader, writer)
}

// inputResult holds the result of reading from stdin.
type inputResult struct {
	input string
	err   error
}

// readUserInput prompts for and reads user input, supporting context cancellation.
// Returns the trimmed input string, or an error if reading fails or context is cancelled.
// Returns io.EOF when the input stream ends (e.g., piped input).
//
// NOTE: The stdin reading goroutine cannot be interrupted once started, as Go's
// bufio.Reader.ReadString blocks until input or EOF. If context is cancelled
// before input arrives, one goroutine will remain blocked until process exit.
// This is a known Go limitation with blocking stdin reads.
func readUserInput(
	ctx context.Context,
	reader *bufio.Reader,
	inputChan chan inputResult,
	writer io.Writer,
) (string, error) {
	_, _ = fmt.Fprint(writer, "You: ")

	go func() {
		input, readErr := reader.ReadString('\n')
		inputChan <- inputResult{input: input, err: readErr}
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("input cancelled: %w", ctx.Err())
	case result := <-inputChan:
		if result.err != nil {
			if errors.Is(result.err, io.EOF) {
				return "", io.EOF
			}

			return "", fmt.Errorf("failed to read input: %w", result.err)
		}

		return strings.TrimSpace(result.input), nil
	}
}

// sendAndDisplayResponse sends a chat message and displays the response.
func sendAndDisplayResponse(
	ctx context.Context,
	session *copilot.Session,
	input string,
	streaming bool,
	timeout time.Duration,
	writer io.Writer,
) error {
	_, _ = fmt.Fprint(writer, "\nAssistant: ")

	var sendErr error
	if streaming {
		sendErr = sendChatWithStreaming(ctx, session, input, timeout, writer)
	} else {
		sendErr = sendChatWithoutStreaming(ctx, session, input, timeout, writer)
	}

	if sendErr != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("chat interrupted: %w", ctx.Err())
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: "Error: " + sendErr.Error(),
			Writer:  writer,
		})
	}

	_, _ = fmt.Fprintln(writer, "")

	return nil
}

// runChatInteractiveLoop runs the interactive chat loop.
// It handles user input and AI responses until the user exits or the context is cancelled.
func runChatInteractiveLoop(
	ctx context.Context,
	session *copilot.Session,
	streaming bool,
	timeout time.Duration,
	reader *bufio.Reader,
	writer io.Writer,
) error {
	inputChan := make(chan inputResult, 1)

	for {
		input, err := readUserInput(ctx, reader, inputChan, writer)
		if errors.Is(err, io.EOF) || ctx.Err() != nil {
			//nolint:nilerr // EOF and context cancellation are graceful exit conditions, not errors.
			return nil
		}

		if err != nil {
			return err
		}

		if input == "" {
			continue
		}

		if isExitCommand(input) {
			notify.WriteMessage(notify.Message{
				Type:    notify.InfoType,
				Content: "Chat session ended. Goodbye!",
				Writer:  writer,
			})

			return nil
		}

		sendErr := sendAndDisplayResponse(
			ctx, session, input, streaming, timeout, writer,
		)
		if sendErr != nil {
			return sendErr
		}
	}
}

// isExitCommand checks if the input is an exit command.
func isExitCommand(input string) bool {
	lower := strings.ToLower(input)

	return lower == "exit" || lower == "quit" || lower == "q" || lower == "/exit" ||
		lower == "/quit"
}

// buildNonTUIOnEventHandler creates an OnEvent handler for non-TUI mode that logs
// session-level events not covered by the per-turn streaming handler.
func buildNonTUIOnEventHandler(writer io.Writer) copilot.SessionEventHandler {
	return func(event copilot.SessionEvent) {
		//nolint:exhaustive // Only session-level events not in per-turn handler; rest ignored.
		switch event.Type {
		case copilot.SessionEventTypeToolExecutionProgress:
			if data, ok := event.Data.(*copilot.ToolExecutionProgressData); ok {
				_, _ = fmt.Fprintf(writer, "  ⏳ %s\n", data.ProgressMessage)
			}
		case copilot.SessionEventTypeSessionTaskComplete:
			data, isTaskComplete := event.Data.(*copilot.SessionTaskCompleteData)
			if isTaskComplete && data.Summary != nil {
				_, _ = fmt.Fprintf(writer, "\n✅ %s\n", *data.Summary)
			}
		}
	}
}

// streamingState manages the state of a streaming chat response.
type streamingState struct {
	done        chan struct{}
	responseErr error
	mu          sync.Mutex
	doneOnce    sync.Once
}

// markDone signals that streaming is complete.
// Safe for concurrent callers via sync.Once.
func (s *streamingState) markDone() {
	s.doneOnce.Do(func() { close(s.done) })
}

// streamingAction describes what I/O to perform after releasing the lock.
type streamingAction int

const (
	actionNone         streamingAction = iota
	actionDelta                        // write delta content
	actionToolStart                    // write tool execution start
	actionToolComplete                 // write tool completion
)

// streamingOutput holds the data needed for post-unlock I/O.
type streamingOutput struct {
	action streamingAction
	text   string
}

// handleStreamingEvent processes a single streaming session event.
// State mutation happens under the lock; I/O happens after unlocking.
func handleStreamingEvent(
	event copilot.SessionEvent,
	writer io.Writer,
	state *streamingState,
) {
	output := computeStreamingOutput(event, state)
	writeStreamingOutput(output, writer)
}

// computeStreamingOutput processes state changes under the lock and returns
// the I/O action to perform after unlocking.
//
//nolint:cyclop // type-switch dispatcher for session events
func computeStreamingOutput(event copilot.SessionEvent, state *streamingState) streamingOutput {
	state.mu.Lock()
	defer state.mu.Unlock()

	//nolint:exhaustive // Only a subset of ~30 SDK event types are relevant for streaming display.
	switch event.Type {
	case copilot.SessionEventTypeAssistantMessageDelta:
		if data, ok := event.Data.(*copilot.AssistantMessageDeltaData); ok {
			return streamingOutput{action: actionDelta, text: data.DeltaContent}
		}
	case copilot.SessionEventTypeSessionIdle:
		state.markDone()
	case copilot.SessionEventTypeSessionError:
		if data, ok := event.Data.(*copilot.SessionErrorData); ok {
			state.responseErr = fmt.Errorf("%w: %s", errSessionError, data.Message)
		}

		state.markDone()
	case copilot.SessionEventTypeToolExecutionStart:
		toolName := getToolName(event)
		toolArgs := getToolArgs(event)

		return streamingOutput{
			action: actionToolStart,
			text:   fmt.Sprintf("\n🔧 Running: %s%s\n", toolName, toolArgs),
		}
	case copilot.SessionEventTypeToolExecutionComplete:
		return streamingOutput{action: actionToolComplete}
	case copilot.SessionEventTypeSystemNotification:
		if data, ok := event.Data.(*copilot.SystemNotificationData); ok {
			return streamingOutput{action: actionDelta, text: "\nℹ️ " + data.Content + "\n"}
		}
	case copilot.SessionEventTypeSessionWarning:
		if data, ok := event.Data.(*copilot.SessionWarningData); ok {
			return streamingOutput{action: actionDelta, text: "\n⚠️ " + data.Message + "\n"}
		}
	case copilot.SessionEventTypeToolExecutionProgress:
		if data, ok := event.Data.(*copilot.ToolExecutionProgressData); ok {
			return streamingOutput{
				action: actionDelta,
				text:   "  ⏳ " + data.ProgressMessage + "\n",
			}
		}
	case copilot.SessionEventTypeSessionTaskComplete:
		if data, ok := event.Data.(*copilot.SessionTaskCompleteData); ok && data.Summary != nil {
			return streamingOutput{action: actionDelta, text: "\n✅ " + *data.Summary + "\n"}
		}
	default:
		// Ignore other event types
	}

	return streamingOutput{action: actionNone}
}

// writeStreamingOutput performs the I/O operation outside the critical section.
func writeStreamingOutput(output streamingOutput, writer io.Writer) {
	switch output.action {
	case actionDelta:
		_, _ = fmt.Fprint(writer, output.text)
	case actionToolStart:
		_, _ = fmt.Fprint(writer, output.text)
	case actionToolComplete:
		_, _ = fmt.Fprint(writer, "✓ Done\n")
	case actionNone:
		// Nothing to write
	}
}

// sendChatWithStreaming sends a message and streams the response.
// It respects the context for cancellation and the timeout for maximum response time.
func sendChatWithStreaming(
	ctx context.Context,
	session *copilot.Session,
	input string,
	timeout time.Duration,
	writer io.Writer,
) error {
	state := &streamingState{done: make(chan struct{})}

	unsubscribe := session.On(func(event copilot.SessionEvent) {
		handleStreamingEvent(event, writer, state)
	})
	defer unsubscribe()

	_, err := session.Send(ctx, copilot.MessageOptions{Prompt: input})
	if err != nil {
		return fmt.Errorf("failed to send chat message: %w", err)
	}

	select {
	case <-state.done:
	case <-ctx.Done():
		_ = session.Abort(ctx)

		return fmt.Errorf("streaming cancelled: %w", ctx.Err())
	case <-time.After(timeout):
		_ = session.Abort(ctx)

		return fmt.Errorf("%w after %v", errResponseTimeout, timeout)
	}

	return state.responseErr
}

// sendChatWithoutStreaming sends a message and waits for the complete response.
// The timeout is enforced via a derived context so that SendAndWait (which uses
// context-based cancellation) is bounded in time.
func sendChatWithoutStreaming(
	ctx context.Context,
	session *copilot.Session,
	input string,
	timeout time.Duration,
	writer io.Writer,
) error {
	// Wrap the parent context with a timeout so SendAndWait respects the deadline.
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Use a channel to make the blocking call cancellable
	type result struct {
		response *copilot.SessionEvent
		err      error
	}

	resultChan := make(chan result, 1)

	go func() {
		response, err := session.SendAndWait(timeoutCtx, copilot.MessageOptions{Prompt: input})
		resultChan <- result{response: response, err: err}
	}()

	select {
	case <-timeoutCtx.Done():
		// Abort the in-flight Copilot request when the context is cancelled or timed out.
		_ = session.Abort(ctx)

		if ctx.Err() != nil {
			return fmt.Errorf("chat cancelled: %w", ctx.Err())
		}

		return fmt.Errorf("%w after %v", errResponseTimeout, timeout)
	case chatResult := <-resultChan:
		if chatResult.err != nil {
			return fmt.Errorf("failed to send chat message: %w", chatResult.err)
		}

		if chatResult.response != nil {
			if data, ok := chatResult.response.Data.(*copilot.AssistantMessageData); ok {
				_, _ = fmt.Fprintln(writer, data.Content)
			}
		}

		return nil
	}
}

// getToolName extracts the tool name from a session event.
func getToolName(event copilot.SessionEvent) string {
	if data, ok := event.Data.(*copilot.ToolExecutionStartData); ok {
		return data.ToolName
	}

	return "unknown"
}

// formatArgsMap converts a map of arguments to a comma-separated key=value string.
// Keys are sorted for consistent output across runs.
func formatArgsMap(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}

	// Sort keys for consistent output (Go map iteration order is non-deterministic)
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, args[k]))
	}

	return strings.Join(parts, ", ")
}

// getToolArgs formats tool arguments for display with parentheses.
func getToolArgs(event copilot.SessionEvent) string {
	startData, isStartData := event.Data.(*copilot.ToolExecutionStartData)
	if !isStartData || startData.Arguments == nil {
		return ""
	}

	args, isMap := startData.Arguments.(map[string]any)
	if !isMap || len(args) == 0 {
		return ""
	}

	formatted := formatArgsMap(args)
	if formatted == "" {
		return ""
	}

	return " (" + formatted + ")"
}

// injectForceFlag injects a "force" argument into the tool invocation.
// This skips interactive confirmation prompts when the tool supports --force.
// Only call this after verifying the tool supports force via toolSupportsForce.
func injectForceFlag(invocation copilot.ToolInvocation) copilot.ToolInvocation {
	args, ok := invocation.Arguments.(map[string]any)
	if !ok || args == nil {
		args = map[string]any{}
	}

	args["force"] = true
	invocation.Arguments = args

	return invocation
}

// toolSupportsForce reports whether the tool's parameter schema defines a "force" property.
// This prevents injecting --force into tools that don't accept it, which would cause
// runtime failures for non-consolidated tools that pass all parameters as CLI flags.
func toolSupportsForce(metadata map[string]toolgen.ToolDefinition, toolName string) bool {
	if metadata == nil {
		return false
	}

	meta, metaExists := metadata[toolName]
	if !metaExists || meta.Parameters == nil {
		return false
	}

	propertiesVal, propsExists := meta.Parameters["properties"]
	if !propsExists {
		return false
	}

	properties, propsIsMap := propertiesVal.(map[string]any)
	if !propsIsMap {
		return false
	}

	_, hasForce := properties["force"]

	return hasForce
}

// pathArgKeys returns the argument keys that SDK-managed file tools use for paths.
// Checked in order; the first match is validated.
func pathArgKeys() []string {
	return []string{"path", "filePath", "file", "target", "directory"}
}

// BuildPreToolUseHook creates a PreToolUseHandler that enforces path sandboxing on ALL tool
// invocations (both custom KSail tools and SDK-managed tools like git/shell/filesystem).
// Mode enforcement (plan mode tool blocking) is handled server-side via Session.RPC.Mode.Set().
func BuildPreToolUseHook(
	allowedRoot string,
) copilot.PreToolUseHandler {
	return func(input copilot.PreToolUseHookInput, _ copilot.HookInvocation) (*copilot.PreToolUseHookOutput, error) {
		return validatePathAccess(input, allowedRoot)
	}
}

// validatePathAccess checks whether a tool invocation's file path arguments fall within
// the allowed root directory. Only SDK-managed tools (not in toolMetadata) are checked.
func validatePathAccess(
	input copilot.PreToolUseHookInput,
	allowedRoot string,
) (*copilot.PreToolUseHookOutput, error) {
	if allowedRoot == "" {
		return nil, nil //nolint:nilnil // nil omits "output" key from JSON-RPC response
	}

	args, ok := input.ToolArgs.(map[string]any)
	if !ok || len(args) == 0 {
		return nil, nil //nolint:nilnil // nil omits "output" key from JSON-RPC response
	}

	for _, key := range pathArgKeys() {
		val, exists := args[key]
		if !exists {
			continue
		}

		pathStr, isStr := val.(string)
		if !isStr || pathStr == "" {
			continue
		}

		if !chatsvc.IsPathWithinDirectory(pathStr, allowedRoot) {
			return &copilot.PreToolUseHookOutput{
				PermissionDecision: "deny",
				PermissionDecisionReason: fmt.Sprintf(
					"Access denied — path %q is outside the project directory (%s). "+
						"File access is restricted to the current working directory and its subdirectories.",
					pathStr, allowedRoot,
				),
			}, nil
		}
	}

	return nil, nil //nolint:nilnil // nil omits "output" key from JSON-RPC response
}

// WrapToolsWithForceInjection wraps write tools to inject the --force flag after
// SDK-native permission approval. Permission handling is delegated entirely to the
// SDK's OnPermissionRequest handler — this wrapper only handles force-flag injection.
func WrapToolsWithForceInjection(
	tools []copilot.Tool,
	toolMetadata map[string]toolgen.ToolDefinition,
) []copilot.Tool {
	wrappedTools := make([]copilot.Tool, len(tools))

	for toolIdx, tool := range tools {
		wrappedTools[toolIdx] = tool

		// Create per-iteration copies to avoid closure capture bug.
		// Each handler must use its own tool's name and handler, not the last iteration's values.
		originalHandler := tool.Handler
		toolName := tool.Name

		wrappedTools[toolIdx].Handler = func(invocation copilot.ToolInvocation) (copilot.ToolResult, error) {
			return invokeWithOptionalForce(invocation, toolMetadata, toolName, originalHandler)
		}
	}

	return wrappedTools
}

// invokeWithOptionalForce injects the force flag if the tool supports it, then calls the handler.
func invokeWithOptionalForce(
	invocation copilot.ToolInvocation,
	toolMetadata map[string]toolgen.ToolDefinition,
	toolName string,
	handler func(copilot.ToolInvocation) (copilot.ToolResult, error),
) (copilot.ToolResult, error) {
	if toolSupportsForce(toolMetadata, toolName) {
		invocation = injectForceFlag(invocation)
	}

	return handler(invocation)
}

// startOutputForwarder forwards tool output chunks to the TUI event channel.
// Returns a WaitGroup that completes when the forwarder goroutine exits.
func startOutputForwarder(
	outputChan <-chan toolgen.OutputChunk,
	eventChan chan<- tea.Msg,
) *sync.WaitGroup {
	var forwarderWg sync.WaitGroup

	forwarderWg.Go(func() {
		for chunk := range outputChan {
			eventChan <- chatui.ToolOutputChunkMsg{
				ToolID: chunk.ToolID,
				Chunk:  chunk.Chunk,
			}
		}
	})

	return &forwarderWg
}

// setupChatTools configures the chat tools, permission and mode references.
func setupChatTools(
	sessionConfig *copilot.SessionConfig,
	rootCmd *cobra.Command,
	eventChan chan tea.Msg,
	outputChan chan toolgen.OutputChunk,
	sessionLog *toolgen.SessionLogRef,
) (*chatui.ChatModeRef, error) {
	tools, toolMetadata := chatsvc.GetKSailToolMetadata(rootCmd, outputChan, sessionLog)
	chatModeRef := chatui.NewChatModeRef(chatui.InteractiveMode)
	tools = WrapToolsWithForceInjection(tools, toolMetadata)
	sessionConfig.Tools = tools
	sessionConfig.OnPermissionRequest = chatui.CreateTUIPermissionHandler(eventChan, chatModeRef)

	allowedRoot, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to determine working directory for sandboxing: %w", err)
	}

	sessionConfig.Hooks = &copilot.SessionHooks{
		OnPreToolUse: BuildPreToolUseHook(allowedRoot),
	}

	return chatModeRef, nil
}

// buildTUIOnEventHandler creates an OnEvent handler for the TUI that dispatches
// session-level events to the event channel. It handles events NOT covered by the
// per-turn session.On() dispatcher, ensuring events during session creation and
// between turns are captured.
func buildTUIOnEventHandler(eventChan chan<- tea.Msg) copilot.SessionEventHandler {
	return func(event copilot.SessionEvent) {
		//nolint:exhaustive // Only session-level events not in per-turn dispatcher; rest ignored.
		switch event.Type {
		case copilot.SessionEventTypeToolExecutionProgress:
			if data, ok := event.Data.(*copilot.ToolExecutionProgressData); ok {
				eventChan <- chatui.ToolProgressMsg{
					ToolID:  data.ToolCallID,
					Message: data.ProgressMessage,
				}
			}
		case copilot.SessionEventTypeSessionTaskComplete:
			msg := ""

			data, isTaskComplete := event.Data.(*copilot.SessionTaskCompleteData)
			if isTaskComplete && data.Summary != nil {
				msg = *data.Summary
			}

			eventChan <- chatui.TaskCompleteMsg{Message: msg}
		}
	}
}

// wireSessionLog wires the session's RPC.Log method into the SessionLogRef
// so tool handlers can log to the session during execution.
func wireSessionLog(session *copilot.Session, logRef *toolgen.SessionLogRef) {
	if logRef == nil {
		return
	}

	logRef.Set(func(ctx context.Context, message, level string) {
		l := rpc.Level(level)
		_, _ = session.RPC.Log(ctx, &rpc.SessionLogParams{
			Message: message,
			Level:   &l,
		})
	})
}

// runTUIChat starts the TUI chat mode.
//
//nolint:funlen // session lifecycle setup requires sequential steps
func runTUIChat(
	ctx context.Context,
	client *copilot.Client,
	sessionConfig *copilot.SessionConfig,
	timeout time.Duration,
	rootCmd *cobra.Command,
) error {
	currentModel := sessionConfig.Model
	eventChan := make(chan tea.Msg, eventChannelBuffer)
	outputChan := make(chan toolgen.OutputChunk, outputChannelBuffer)
	forwarderWg := startOutputForwarder(outputChan, eventChan)

	// Create session log ref for SDK-native tool logging
	sessionLog := toolgen.NewSessionLogRef()

	// Register OnEvent handler to catch session-level events during creation and between turns
	sessionConfig.OnEvent = buildTUIOnEventHandler(eventChan)

	chatModeRef, err := setupChatTools( //nolint:contextcheck
		sessionConfig, rootCmd, eventChan, outputChan, sessionLog,
	)
	if err != nil {
		close(outputChan)
		forwarderWg.Wait()

		return err
	}

	// Register slash commands for the TUI
	sessionConfig.Commands = chatui.BuildTUISlashCommands(eventChan)

	// Register elicitation handler for MCP tool form requests
	sessionConfig.OnElicitationRequest = chatui.CreateTUIElicitationHandler(eventChan)

	session, err := client.CreateSession(ctx, sessionConfig)
	if err != nil {
		close(outputChan)
		forwarderWg.Wait()

		return fmt.Errorf("failed to create chat session: %w", err)
	}

	// Wire session log now that the session exists
	wireSessionLog(session, sessionLog)

	defer func() {
		close(outputChan)
		forwarderWg.Wait()

		select {
		case <-ctx.Done():
			os.Exit(signalExitCode)
		default:
			_ = session.Disconnect()
		}
	}()

	err = chatui.Run(ctx, chatui.Params{
		Session:       session,
		Client:        client,
		SessionConfig: sessionConfig,
		Models:        nil, // Lazy-loaded on first ^O press
		CurrentModel:  currentModel,
		Timeout:       timeout,
		EventChan:     eventChan,
		ChatModeRef:   chatModeRef,
		Theme:         chatui.DefaultThemeConfig(),
		ToolDisplay:   chatui.DefaultToolDisplayConfig(),
	})
	if err != nil {
		return fmt.Errorf("TUI chat failed: %w", err)
	}

	return nil
}
