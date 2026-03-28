package kubeconform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/yannh/kubeconform/pkg/validator"
)

// ErrValidationFailed indicates that validation failed.
var ErrValidationFailed = errors.New("validation failed")

// Client provides kubeconform validation functionality.
type Client struct{}

// NewClient creates a new kubeconform client.
func NewClient() *Client {
	return &Client{}
}

// ValidateFile validates a single Kubernetes manifest file.
func (c *Client) ValidateFile(ctx context.Context, filePath string, opts *ValidationOptions) error {
	if opts == nil {
		opts = &ValidationOptions{}
	}

	// Check context before starting
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Open the file
	file, err := os.Open(filePath) //nolint:gosec // filePath is provided by the caller
	if err != nil {
		return fmt.Errorf("open file %s: %w", filePath, err)
	}

	defer func() {
		_ = file.Close()
	}()

	// Create validator
	kubeValidator, err := c.createValidator(opts)
	if err != nil {
		return fmt.Errorf("create validator: %w", err)
	}

	// Validate resources from file
	results := kubeValidator.Validate(filePath, file)

	// Check for validation errors
	return c.processResults(results, filePath)
}

// ValidateManifests validates Kubernetes manifests from a reader (e.g., kustomize build output).
func (c *Client) ValidateManifests(
	ctx context.Context,
	reader io.Reader,
	opts *ValidationOptions,
) error {
	if opts == nil {
		opts = &ValidationOptions{}
	}

	// Check context before starting
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Create validator
	kubeValidator, err := c.createValidator(opts)
	if err != nil {
		return fmt.Errorf("create validator: %w", err)
	}

	// Wrap reader in ReadCloser
	rc := io.NopCloser(reader)

	// Validate resources from reader
	results := kubeValidator.Validate("stdin", rc)

	// Check for validation errors
	return c.processResults(results, "stdin")
}

// processResults processes validation results and returns an error if validation failed.
// Error details are included in the returned error message (not written to stderr)
// to avoid interleaving with ProgressGroup's ANSI output.
func (c *Client) processResults(results []validator.Result, source string) error {
	var errDetails []string

	for _, res := range results {
		if res.Status == validator.Invalid || res.Status == validator.Error {
			errDetails = append(errDetails, fmt.Sprintf("%v", res.Err))
		}
	}

	if len(errDetails) > 0 {
		return fmt.Errorf("%w: %s: %s", ErrValidationFailed, source, strings.Join(errDetails, "; "))
	}

	return nil
}

// ValidationOptions configures validation behavior.
type ValidationOptions struct {
	// SkipKinds is a list of Kubernetes kinds to skip during validation (e.g., "Secret").
	SkipKinds []string
	// Strict enables strict validation mode.
	Strict bool
	// IgnoreMissingSchemas ignores resources with missing schemas.
	IgnoreMissingSchemas bool
}

// createValidator creates a kubeconform validator with the given options.
func (c *Client) createValidator(opts *ValidationOptions) (validator.Validator, error) {
	// Create schema locations
	schemaLocations := []string{
		// Default Kubernetes schemas
		"default",
		// Add Datree CRDs catalog for additional CRD schemas
		"https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/" +
			"{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json",
	}

	// Convert skip kinds to map
	skipKinds := make(map[string]struct{})
	for _, kind := range opts.SkipKinds {
		skipKinds[kind] = struct{}{}
	}

	// Set up schema cache directory
	var cacheDir string

	if userCacheDir, err := os.UserCacheDir(); err == nil {
		cacheDir = filepath.Join(userCacheDir, "ksail", "kubeconform")

		if err := os.MkdirAll(cacheDir, 0o700); err != nil {
			return nil, fmt.Errorf("create schema cache directory: %w", err)
		}
	} else {
		tmpDir, mkErr := os.MkdirTemp("", "ksail-kubeconform-*")
		if mkErr != nil {
			return nil, fmt.Errorf("create temporary schema cache directory: %w", mkErr)
		}

		cacheDir = tmpDir
	}

	// Create validator options
	validatorOpts := validator.Opts{
		SkipKinds:            skipKinds,
		RejectKinds:          make(map[string]struct{}),
		KubernetesVersion:    "master",
		Strict:               opts.Strict,
		IgnoreMissingSchemas: opts.IgnoreMissingSchemas,
		SkipTLS:              false,
		Cache:                cacheDir,
		Debug:                false,
	}

	v, err := validator.New(schemaLocations, validatorOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create validator: %w", err)
	}

	return v, nil
}
