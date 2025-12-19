package kubeconform

import (
	"context"
	"fmt"
	"io"
	"os/exec"
)

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

	args := c.buildArgs(opts)
	args = append(args, filePath)

	cmd := exec.CommandContext(ctx, "kubeconform", args...) //nolint:gosec // kubeconform is a trusted tool

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("validate file %s: %w\n%s", filePath, err, string(output))
	}

	return nil
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

	args := c.buildArgs(opts)

	cmd := exec.CommandContext(ctx, "kubeconform", args...) //nolint:gosec // kubeconform is a trusted tool
	cmd.Stdin = reader

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("validate manifests: %w\n%s", err, string(output))
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

// buildArgs builds kubeconform command arguments from validation options.
func (c *Client) buildArgs(opts *ValidationOptions) []string {
	args := []string{}

	// Add skip kinds
	for _, kind := range opts.SkipKinds {
		args = append(args, "-skip", kind)
	}

	// Add strict mode
	if opts.Strict {
		args = append(args, "-strict")
	}

	// Add ignore missing schemas
	if opts.IgnoreMissingSchemas {
		args = append(args, "-ignore-missing-schemas")
	}

	// Add verbose mode
	if opts.Verbose {
		args = append(args, "-verbose")
	}

	// Add schema locations
	args = append(args, "-schema-location", "default")
	// Add Datree CRDs catalog for additional CRD schemas
	args = append(
		args,
		"-schema-location",
		"https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json",
	)

	return args
}

