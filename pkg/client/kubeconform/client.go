package kubeconform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

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
func (c *Client) ValidateFile(_ context.Context, filePath string, opts *ValidationOptions) error {
	if opts == nil {
		opts = &ValidationOptions{}
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
	return c.processResults(results, filePath, opts.Verbose)
}

// ValidateManifests validates Kubernetes manifests from a reader (e.g., kustomize build output).
func (c *Client) ValidateManifests(
	_ context.Context,
	reader io.Reader,
	opts *ValidationOptions,
) error {
	if opts == nil {
		opts = &ValidationOptions{}
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
	return c.processResults(results, "stdin", opts.Verbose)
}

// processResults processes validation results and returns an error if validation failed.
func (c *Client) processResults(results []validator.Result, source string, verbose bool) error {
	hasErrors := false

	for _, res := range results {
		if res.Status == validator.Invalid || res.Status == validator.Error {
			hasErrors = true

			if verbose {
				_, _ = fmt.Fprintf(os.Stderr, "❌ %s: %v\n", source, res.Err)
			}
		} else if res.Status == validator.Valid && verbose {
			_, _ = fmt.Fprintf(os.Stdout, "✅ %s is valid\n", source)
		}
	}

	if hasErrors {
		return fmt.Errorf("%w for %s", ErrValidationFailed, source)
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
	// Verbose enables verbose output.
	Verbose bool
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

	// Create validator options
	validatorOpts := validator.Opts{
		SkipKinds:            skipKinds,
		RejectKinds:          make(map[string]struct{}),
		KubernetesVersion:    "master",
		Strict:               opts.Strict,
		IgnoreMissingSchemas: opts.IgnoreMissingSchemas,
		SkipTLS:              false,
		Cache:                "",
		Debug:                false,
	}

	v, err := validator.New(schemaLocations, validatorOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create validator: %w", err)
	}

	return v, nil
}
