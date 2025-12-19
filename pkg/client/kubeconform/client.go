package kubeconform

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/yannh/kubeconform/pkg/validator"
)

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
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file %s: %w", filePath, err)
	}
	defer file.Close()

	// Create validator
	v, err := c.createValidator(opts)
	if err != nil {
		return fmt.Errorf("create validator: %w", err)
	}

	// Validate resources from file
	results := v.Validate(filePath, file)

	// Check for validation errors
	hasErrors := false
	for _, res := range results {
		if res.Status == validator.Invalid || res.Status == validator.Error {
			hasErrors = true
			if opts.Verbose {
				fmt.Printf("❌ %s: %v\n", filePath, res.Err)
			}
		} else if res.Status == validator.Valid && opts.Verbose {
			fmt.Printf("✅ %s is valid\n", filePath)
		}
	}

	if hasErrors {
		return fmt.Errorf("validation failed for %s", filePath)
	}

	return nil
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
	v, err := c.createValidator(opts)
	if err != nil {
		return fmt.Errorf("create validator: %w", err)
	}

	// Wrap reader in ReadCloser
	rc := io.NopCloser(reader)

	// Validate resources from reader
	results := v.Validate("stdin", rc)

	// Check for validation errors
	hasErrors := false
	for _, res := range results {
		if res.Status == validator.Invalid || res.Status == validator.Error {
			hasErrors = true
			if opts.Verbose {
				fmt.Printf("❌ validation error: %v\n", res.Err)
			}
		} else if res.Status == validator.Valid && opts.Verbose {
			fmt.Printf("✅ resource is valid\n")
		}
	}

	if hasErrors {
		return fmt.Errorf("validation failed")
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
		"https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json",
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

	return validator.New(schemaLocations, validatorOpts)
}

