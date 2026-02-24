package oci

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/netretry"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// Registry verification errors.
var (
	// ErrRegistryUnreachable is returned when the registry cannot be reached.
	ErrRegistryUnreachable = errors.New("registry is unreachable")
	// ErrRegistryAuthRequired is returned when authentication is required but not provided.
	ErrRegistryAuthRequired = errors.New(
		"registry requires authentication\n" +
			"  - configure credentials with --local-registry user:pass@host/repo",
	)
	// ErrRegistryPermissionDenied is returned when credentials are invalid or lack write access.
	ErrRegistryPermissionDenied = errors.New(
		"registry access denied\n" +
			"  - check credentials have write permission to the repository",
	)
	// ErrRegistryNotFound is returned when the registry or repository doesn't exist.
	ErrRegistryNotFound = errors.New(
		"registry or repository not found\n" +
			"  - verify the registry URL is correct",
	)
)

// RegistryVerifier checks access to OCI registries.
type RegistryVerifier interface {
	// VerifyAccess checks if the registry is accessible and we have push permissions.
	// Returns nil if access is verified, or an actionable error if not.
	VerifyAccess(ctx context.Context, opts VerifyOptions) error

	// ArtifactExists checks if an artifact with the given tag exists in the repository.
	// Returns true if the artifact exists, false if it doesn't exist.
	// Returns an error if the registry check fails for reasons other than "not found".
	ArtifactExists(ctx context.Context, opts ArtifactExistsOptions) (bool, error)
}

// VerifyOptions contains options for verifying registry access.
type VerifyOptions struct {
	// RegistryEndpoint is the registry host[:port] (e.g., "ghcr.io" or "localhost:5000").
	RegistryEndpoint string
	// Repository is the repository path to check access for.
	Repository string
	// Username is the optional username for authentication.
	Username string
	// Password is the optional password/token for authentication.
	Password string
	// Insecure allows HTTP connections (for local registries).
	Insecure bool
}

// ArtifactExistsOptions contains options for checking if an artifact exists.
type ArtifactExistsOptions struct {
	// RegistryEndpoint is the registry host[:port] (e.g., "ghcr.io" or "localhost:5000").
	RegistryEndpoint string
	// Repository is the repository path.
	Repository string
	// Tag is the artifact tag to check (e.g., "dev", "latest").
	Tag string
	// Username is the optional username for authentication.
	Username string
	// Password is the optional password/token for authentication.
	Password string
	// Insecure allows HTTP connections (for local registries).
	Insecure bool
}

// verifier implements RegistryVerifier.
type verifier struct{}

// NewRegistryVerifier creates a new registry verifier.
func NewRegistryVerifier() RegistryVerifier {
	return &verifier{}
}

// VerifyAccess checks if we can access the registry and have write permissions.
// It attempts to check the repository catalog/tags to verify read access,
// and the error response helps determine if auth is required.
func (v *verifier) VerifyAccess(ctx context.Context, opts VerifyOptions) error {
	if opts.RegistryEndpoint == "" {
		return ErrRegistryEndpointRequired
	}

	repo, err := v.buildRepository(opts)
	if err != nil {
		return err
	}

	remoteOpts := v.buildRemoteOptions(ctx, opts)

	// Try to list tags in the repository.
	// This will tell us if:
	// - Registry is reachable
	// - We have read access (for existing repos)
	// - Auth is required
	//
	// Note: For push verification, we can't actually test push without pushing.
	// But checking read access gives us good signal about auth issues.
	_, err = remote.List(repo, remoteOpts...)
	if err != nil {
		return classifyRegistryError(err)
	}

	return nil
}

// ArtifactExists checks if an artifact with the given tag exists in the repository.
// Returns true if the artifact exists, false if it doesn't exist or the repo doesn't exist.
// Returns an error only for unexpected failures (network issues, auth failures, etc.).
func (v *verifier) ArtifactExists(ctx context.Context, opts ArtifactExistsOptions) (bool, error) {
	if opts.RegistryEndpoint == "" {
		return false, ErrRegistryEndpointRequired
	}

	if opts.Tag == "" {
		return false, ErrVersionRequired
	}

	ref, err := v.buildReference(opts)
	if err != nil {
		return false, err
	}

	remoteOpts := v.buildRemoteOptionsForArtifact(ctx, opts)

	// Try to get the image descriptor - this checks if the specific tag exists
	_, err = remote.Head(ref, remoteOpts...)
	if err != nil {
		// Check if it's a "not found" error - that means the artifact doesn't exist
		if isNotFoundError(err) {
			return false, nil
		}
		// For other errors (auth, network), return them
		return false, classifyRegistryError(err)
	}

	return true, nil
}

// buildReference creates a reference from artifact exists options.
func (v *verifier) buildReference(opts ArtifactExistsOptions) (name.Reference, error) {
	refStr := fmt.Sprintf("%s/%s:%s", opts.RegistryEndpoint, opts.Repository, opts.Tag)

	nameOpts := []name.Option{name.WeakValidation}
	if opts.Insecure {
		nameOpts = append(nameOpts, name.Insecure)
	}

	ref, err := name.ParseReference(refStr, nameOpts...)
	if err != nil {
		return nil, fmt.Errorf("parse reference: %w", err)
	}

	return ref, nil
}

// buildRemoteOptionsForArtifact creates remote options for artifact existence check.
func (v *verifier) buildRemoteOptionsForArtifact(
	ctx context.Context,
	opts ArtifactExistsOptions,
) []remote.Option {
	return buildRemoteOptionsWithAuth(ctx, opts.Username, opts.Password)
}

// isNotFoundError checks if the error indicates the artifact doesn't exist.
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	var transportErr *transport.Error
	if errors.As(err, &transportErr) {
		if transportErr.StatusCode == http.StatusNotFound {
			return true
		}
	}

	errStr := strings.ToLower(err.Error())

	return strings.Contains(errStr, "not found") ||
		strings.Contains(errStr, "manifest unknown") ||
		strings.Contains(errStr, "name_unknown") ||
		strings.Contains(errStr, "name unknown")
}

// buildRepository creates a repository reference from the verify options.
func (v *verifier) buildRepository(opts VerifyOptions) (name.Repository, error) {
	refStr := fmt.Sprintf("%s/%s", opts.RegistryEndpoint, opts.Repository)

	nameOpts := []name.Option{name.WeakValidation}
	if opts.Insecure {
		nameOpts = append(nameOpts, name.Insecure)
	}

	repo, err := name.NewRepository(refStr, nameOpts...)
	if err != nil {
		return name.Repository{}, fmt.Errorf("parse repository reference: %w", err)
	}

	return repo, nil
}

// buildRemoteOptions creates remote options for registry operations.
func (v *verifier) buildRemoteOptions(ctx context.Context, opts VerifyOptions) []remote.Option {
	return buildRemoteOptionsWithAuth(ctx, opts.Username, opts.Password)
}

// buildRemoteOptionsWithAuth creates remote options with optional basic auth.
func buildRemoteOptionsWithAuth(ctx context.Context, username, password string) []remote.Option {
	remoteOpts := []remote.Option{
		remote.WithContext(ctx),
	}

	if username != "" || password != "" {
		auth := &authn.Basic{
			Username: username,
			Password: password,
		}
		remoteOpts = append(remoteOpts, remote.WithAuth(auth))
	}

	return remoteOpts
}

// classifyTransportError handles HTTP transport errors.
func classifyTransportError(transportErr *transport.Error) error {
	switch transportErr.StatusCode {
	case http.StatusUnauthorized:
		return ErrRegistryAuthRequired
	case http.StatusForbidden:
		return ErrRegistryPermissionDenied
	case http.StatusNotFound:
		// 404 for tags list is OK - repo might not exist yet
		// This is fine for push operations
		return nil
	default:
		return nil
	}
}

// classifyErrorByMessage checks error message patterns.
// Returns:
//   - matched=true, error: if the error matches a known error pattern
//   - matched=true, nil: if the error matches an "acceptable" pattern (e.g., repo doesn't exist yet)
//   - matched=false, nil: if no pattern matched
func classifyErrorByMessage(errStr string) (bool, error) {
	lowerErr := strings.ToLower(errStr)

	switch {
	case strings.Contains(lowerErr, "unauthorized"),
		strings.Contains(lowerErr, "authentication required"):
		return true, ErrRegistryAuthRequired

	case strings.Contains(lowerErr, "denied"),
		strings.Contains(lowerErr, "forbidden"):
		return true, ErrRegistryPermissionDenied

	case strings.Contains(lowerErr, "no such host"),
		strings.Contains(lowerErr, "connection refused"),
		strings.Contains(lowerErr, "dial tcp"):
		return true, fmt.Errorf("%w: %s", ErrRegistryUnreachable, extractErrorDetail(errStr))

	case strings.Contains(lowerErr, "not found"),
		strings.Contains(lowerErr, "name_unknown"),
		strings.Contains(lowerErr, "name unknown"):
		// Not found / NAME_UNKNOWN is OK - we can still push to create it
		return true, nil

	default:
		return false, nil
	}
}

// classifyRegistryError converts low-level registry errors to actionable errors.
func classifyRegistryError(err error) error {
	if err == nil {
		return nil
	}

	// Check for transport errors (includes HTTP status codes)
	var transportErr *transport.Error
	if errors.As(err, &transportErr) {
		classifiedErr := classifyTransportError(transportErr)
		if classifiedErr != nil {
			return classifiedErr
		}
	}

	// Check for common error patterns in the message
	matched, classifiedErr := classifyErrorByMessage(err.Error())
	if matched {
		// If matched, return the classified error (which may be nil for acceptable errors)
		return classifiedErr
	}

	// Return original error with context
	return fmt.Errorf("registry access check failed: %w", err)
}

// extractErrorDetail extracts the most useful part of an error message.
func extractErrorDetail(errStr string) string {
	// Try to extract just the useful part
	if idx := strings.Index(errStr, ": "); idx > 0 {
		return errStr[idx+2:]
	}

	return errStr
}

// Registry verification retry constants.
const (
	verifyMaxRetries    = 3
	verifyRetryBaseWait = 2 * time.Second
	verifyRetryMaxWait  = 10 * time.Second
)

// VerifyRegistryAccessWithTimeout verifies registry access with a timeout.
// Retries transient network errors (e.g., timeouts, 5xx) with exponential backoff.
func VerifyRegistryAccessWithTimeout(
	ctx context.Context,
	opts VerifyOptions,
	timeout time.Duration,
) error {
	verifier := NewRegistryVerifier()

	var lastErr error

	for attempt := 1; attempt <= verifyMaxRetries; attempt++ {
		verifyCtx, cancel := context.WithTimeout(ctx, timeout)

		err := verifier.VerifyAccess(verifyCtx, opts)
		cancel()

		if err == nil {
			return nil
		}

		lastErr = err

		if !netretry.IsRetryable(lastErr) || attempt == verifyMaxRetries {
			break
		}

		delay := netretry.ExponentialDelay(attempt, verifyRetryBaseWait, verifyRetryMaxWait)

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}

			return fmt.Errorf("registry access verification cancelled: %w", ctx.Err())
		case <-timer.C:
		}
	}

	return fmt.Errorf("registry access verification failed: %w", lastErr)
}
