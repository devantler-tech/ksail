// Package awssso renews supported kubeconfig AWS SSO sessions through the AWS CLI.
package awssso

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

var (
	// ErrUnsupported means the context does not use a supported direct AWS SSO profile.
	ErrUnsupported = errors.New("this context does not use a supported direct AWS SSO profile")
	// ErrCredentialCheck reports a failed credential probe without exposing subprocess output.
	ErrCredentialCheck = errors.New("AWS credential check failed; no SSO renewal was performed")
	// ErrLoginFailed reports a failed provider sign-in without exposing its output.
	ErrLoginFailed = errors.New("AWS SSO sign-in failed")
	// ErrStillExpired means sign-in returned but the configured credentials remain unavailable.
	ErrStillExpired = errors.New("AWS SSO credentials remain unavailable after sign-in")
	// ErrCLIUnavailable means the configured AWS executable cannot be found.
	ErrCLIUnavailable = errors.New("AWS CLI is unavailable on the application PATH")
)

const (
	probeTimeout = 15 * time.Second
	loginTimeout = 5 * time.Minute
	outputLimit  = 128 * 1024
)

// Target is an immutable selection derived from the kubeconfig's credential command.
type Target struct {
	command  string
	args     []string
	env      []string
	profile  string
	caBundle string
	key      string
	version  string
}

// Manager coalesces concurrent authorized renewals of the same SSO session.
type Manager struct {
	mutex   sync.Mutex
	flights map[string]*flight
}

// flight is one shared sign-in. It outlives any single caller, but not the last one waiting.
type flight struct {
	cancel  context.CancelFunc
	done    chan struct{}
	err     error
	waiters int
}

// Resolve selects the exact exec profile and shared files without retrieving SDK credentials.
func Resolve(ctx context.Context, provider *clientcmdapi.ExecConfig) (*Target, error) {
	if !awsProvider(provider) {
		return nil, ErrUnsupported
	}

	profileArg, err := parseArgs(provider.Args)
	if err != nil {
		return nil, err
	}

	env := snapshotEnv(provider.Env)

	values := envValues(env)
	if profileArg == "" && hasAmbientCredentials(values) {
		return nil, ErrUnsupported
	}

	profile := effectiveProfile(profileArg, values)
	home := effectiveHome(values)

	if home == "" {
		return nil, ErrUnsupported
	}

	shared, err := selectedSharedConfig(ctx, profile, home, values)
	if err != nil {
		return nil, fmt.Errorf("read selected AWS SSO profile configuration: %w", ErrUnsupported)
	}

	if hasAlternativeProvider(shared) {
		return nil, ErrUnsupported
	}

	command, err := exec.LookPath(provider.Command)
	if err != nil {
		return nil, ErrCLIUnavailable
	}

	identity, err := sessionIdentity(shared, home)
	if err != nil {
		return nil, fmt.Errorf("encode AWS SSO session identity: %w", err)
	}

	return &Target{
		command:  command,
		args:     slices.Clone(provider.Args),
		env:      env,
		profile:  profile,
		caBundle: optionValue(provider.Args, "--ca-bundle"),
		key:      identity,
		version:  provider.APIVersion,
	}, nil
}

// Expired probes the configured credential command; it never initiates sign-in.
func (target *Target) Expired(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	stdout, stderr, err := target.run(ctx, target.args)
	if err != nil {
		if ctx.Err() != nil {
			return false, fmt.Errorf("credential probe cancelled: %w", ctx.Err())
		}

		if isExpiredSSO(stderr) {
			return true, nil
		}

		return false, ErrCredentialCheck
	}

	var credential struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Status     struct {
			Token string `json:"token"`
		} `json:"status"`
	}

	err = json.Unmarshal(stdout, &credential)
	if err != nil ||
		credential.Kind != "ExecCredential" || credential.Status.Token == "" ||
		credential.APIVersion != target.version {
		return false, ErrCredentialCheck
	}

	return false, nil
}

// Renew initiates the AWS CLI browser flow only after the caller has obtained user consent.
// Callers must gate this operation on their explicit local/interactive experimental setting.
func (manager *Manager) Renew(ctx context.Context, target *Target) error {
	return manager.renew(ctx, target, nil)
}

// RenewWithDeviceCode is Renew for a caller that owns a terminal: the AWS CLI's device-code
// instructions go to that terminal, so sign-in can be completed from another device. The provider
// output is written to the terminal only and never becomes part of the returned error.
// It requires AWS CLI 2.22 or newer.
func (manager *Manager) RenewWithDeviceCode(
	ctx context.Context,
	target *Target,
	terminal io.Writer,
) error {
	return manager.renew(ctx, target, terminal)
}

func (manager *Manager) renew(ctx context.Context, target *Target, terminal io.Writer) error {
	key := target.key
	if terminal != nil {
		key = "device-code\x00" + key
	}

	current := manager.join(ctx, key, target, terminal)

	select {
	case <-current.done:
		return current.err
	case <-ctx.Done():
		manager.leave(key, current)

		return fmt.Errorf("AWS sign-in cancelled: %w", ctx.Err())
	}
}

// join adds the caller to the session's sign-in, starting one when none is in progress.
func (manager *Manager) join(
	ctx context.Context,
	key string,
	target *Target,
	terminal io.Writer,
) *flight {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	current, found := manager.flights[key]
	if !found {
		// The sign-in owns its context: the caller that started it may leave while others still wait.
		flightCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), loginTimeout)
		current = &flight{cancel: cancel, done: make(chan struct{})}

		if manager.flights == nil {
			manager.flights = make(map[string]*flight)
		}

		manager.flights[key] = current

		go manager.complete(flightCtx, key, current, target, terminal)
	}

	current.waiters++

	return current
}

func (manager *Manager) complete(
	ctx context.Context,
	key string,
	current *flight,
	target *Target,
	terminal io.Writer,
) {
	err := target.signIn(ctx, terminal)

	manager.forget(key, current)

	current.err = err
	current.cancel()
	close(current.done)
}

// leave removes a cancelled caller and stops a sign-in nobody is waiting for any more.
func (manager *Manager) leave(key string, current *flight) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	current.waiters--
	if current.waiters > 0 {
		return
	}

	current.cancel()

	if manager.flights[key] == current {
		delete(manager.flights, key)
	}
}

func (manager *Manager) forget(key string, current *flight) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	if manager.flights[key] == current {
		delete(manager.flights, key)
	}
}

func (target *Target) signIn(ctx context.Context, terminal io.Writer) error {
	expired, err := target.Expired(ctx)
	if err != nil || !expired {
		return err
	}

	args := []string{
		"sso", "login", "--profile=" + target.profile, "--no-cli-pager", "--no-cli-auto-prompt",
	}
	if target.caBundle != "" {
		args = append(args, "--ca-bundle="+target.caBundle)
	}

	if terminal != nil {
		// The default flow redirects to the signing-in host, so it cannot finish from another device.
		args = append(args, "--use-device-code")
	}

	err = target.login(ctx, args, terminal)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("provider sign-in cancelled: %w", ctx.Err())
		}

		return ErrLoginFailed
	}

	expired, err = target.Expired(ctx)
	if err != nil || expired {
		return ErrStillExpired
	}

	return nil
}

func (target *Target) login(ctx context.Context, args []string, terminal io.Writer) error {
	if terminal == nil {
		_, _, err := target.run(ctx, args)

		return err
	}

	command := target.prepare(ctx, args)
	command.Stdout, command.Stderr = terminal, terminal

	return command.Run() //nolint:wrapcheck // signIn replaces this with a sanitized error.
}

func (target *Target) run(ctx context.Context, args []string) ([]byte, string, error) {
	command := target.prepare(ctx, args)
	stdout, stderr := &boundedOutput{}, &boundedOutput{}
	command.Stdout, command.Stderr = stdout, stderr
	err := command.Run()

	if stdout.truncated || stderr.truncated {
		return nil, "", ErrCredentialCheck
	}

	return stdout.Bytes(), stderr.String(), err
}

func (target *Target) prepare(ctx context.Context, args []string) *exec.Cmd {
	//nolint:gosec // The executable and args are frozen from the user's selected AWS exec configuration.
	command := exec.CommandContext(ctx, target.command, args...)
	command.Env = target.env
	command.WaitDelay = time.Second

	return command
}

type boundedOutput struct {
	buffer    bytes.Buffer
	truncated bool
}

func (output *boundedOutput) Bytes() []byte  { return output.buffer.Bytes() }
func (output *boundedOutput) String() string { return output.buffer.String() }

func (output *boundedOutput) Write(value []byte) (int, error) {
	size := len(value)

	remaining := outputLimit - output.buffer.Len()
	if size > remaining {
		value = value[:remaining]
		output.truncated = true
	}

	_, err := output.buffer.Write(value)
	if err != nil {
		return size, fmt.Errorf("capture AWS output: %w", err)
	}

	return size, nil
}

func isExpiredSSO(stderr string) bool {
	message := strings.ToLower(stderr)

	return strings.Contains(message, "sso session") && strings.Contains(message, "expired") ||
		strings.Contains(message, "error when retrieving token from sso") &&
			strings.Contains(message, "token has expired")
}

func snapshotEnv(overrides []clientcmdapi.ExecEnvVar) []string {
	values := envValues(os.Environ())
	for _, override := range overrides {
		values[override.Name] = override.Value
	}
	// The command already has all arguments; never let AWS auto-prompt or a pager block a probe.
	values["AWS_CLI_AUTO_PROMPT"], values["AWS_PAGER"] = "off", ""

	env := make([]string, 0, len(values))
	for key, value := range values {
		env = append(env, key+"="+value)
	}

	slices.Sort(env)

	return env
}

func envValues(env []string) map[string]string {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}

	return values
}

func parseArgs(args []string) (string, error) {
	profile, service := "", false

	for index := 0; index < len(args); index++ {
		arg := args[index]
		if !service && arg == "eks" {
			if index+1 >= len(args) || args[index+1] != "get-token" {
				return "", ErrUnsupported
			}

			service = true
			index++

			continue
		}

		flag, value, next, err := readOption(args, index, service)
		if err != nil {
			return "", err
		}

		index = next

		if flag == "--profile" {
			profile = value
		}
	}

	if !service {
		return "", ErrUnsupported
	}

	return profile, nil
}

func optionValue(args []string, wanted string) string {
	value := ""

	for index, arg := range args {
		flag, option, equals := strings.Cut(arg, "=")
		if flag != wanted {
			continue
		}

		if equals {
			value = option
		} else if index+1 < len(args) {
			value = args[index+1]
		}
	}

	return value
}

func hasAlternativeProvider(shared config.SharedConfig) bool {
	return shared.Credentials.HasKeys() || shared.RoleARN != "" || shared.SourceProfileName != "" ||
		shared.CredentialSource != "" || shared.CredentialProcess != "" || shared.WebIdentityTokenFile != ""
}

func awsProvider(provider *clientcmdapi.ExecConfig) bool {
	return provider != nil &&
		slices.Contains(
			[]string{"aws", "aws.exe"},
			strings.ToLower(filepath.Base(provider.Command)),
		)
}

func hasAmbientCredentials(values map[string]string) bool {
	return values["AWS_ACCESS_KEY_ID"] != "" || values["AWS_SECRET_ACCESS_KEY"] != "" ||
		values["AWS_SESSION_TOKEN"] != "" || values["AWS_WEB_IDENTITY_TOKEN_FILE"] != ""
}

func effectiveProfile(explicit string, values map[string]string) string {
	for _, profile := range []string{explicit, values["AWS_DEFAULT_PROFILE"], values["AWS_PROFILE"]} {
		if profile != "" {
			return profile
		}
	}

	return "default"
}

func effectiveHome(values map[string]string) string {
	if values["HOME"] != "" {
		return values["HOME"]
	}

	return values["USERPROFILE"]
}

func selectedSharedConfig(
	ctx context.Context,
	profile, home string,
	values map[string]string,
) (config.SharedConfig, error) {
	configFile := values["AWS_CONFIG_FILE"]
	if configFile == "" {
		configFile = filepath.Join(home, ".aws", "config")
	}

	credentialsFile := values["AWS_SHARED_CREDENTIALS_FILE"]
	if credentialsFile == "" {
		credentialsFile = filepath.Join(home, ".aws", "credentials")
	}

	shared, err := config.LoadSharedConfigProfile(
		ctx,
		profile,
		func(options *config.LoadSharedConfigOptions) {
			options.ConfigFiles = []string{configFile}
			options.CredentialsFiles = []string{credentialsFile}
		},
	)
	if err != nil {
		return config.SharedConfig{}, fmt.Errorf("read selected AWS profile: %w", ErrUnsupported)
	}

	return shared, nil
}

func readOption(args []string, index int, service bool) (string, string, int, error) {
	flag, value, equals := strings.Cut(args[index], "=")

	allowed := slices.Contains([]string{"--profile", "--region", "--output", "--ca-bundle"}, flag)
	if service {
		allowed = allowed ||
			slices.Contains([]string{"--cluster-name", "--cluster-id", "--role-arn"}, flag)
	}

	if !allowed {
		return "", "", index, ErrUnsupported
	}

	if !equals {
		index++
		if index >= len(args) || strings.HasPrefix(args[index], "--") {
			return "", "", index, ErrUnsupported
		}

		value = args[index]
	}

	if value == "" {
		return "", "", index, ErrUnsupported
	}

	return flag, value, index, nil
}

func sessionIdentity(shared config.SharedConfig, home string) (string, error) {
	session, startURL, region := shared.SSOSessionName, shared.SSOStartURL, shared.SSORegion
	if session != "" {
		if shared.SSOSession == nil {
			return "", ErrUnsupported
		}

		startURL, region = shared.SSOSession.SSOStartURL, shared.SSOSession.SSORegion
	}

	if startURL == "" || region == "" || shared.SSOAccountID == "" || shared.SSORoleName == "" {
		return "", ErrUnsupported
	}

	identity, err := json.Marshal([]string{filepath.Clean(home), session, startURL, region})
	if err != nil {
		return "", fmt.Errorf("encode SSO session: %w", err)
	}

	return string(identity), nil
}
