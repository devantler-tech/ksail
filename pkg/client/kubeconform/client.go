package kubeconform

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// FluxCRDSchemasURL is the URL to download Flux CRD schemas.
	FluxCRDSchemasURL = "https://github.com/fluxcd/flux2/releases/latest/download/crd-schemas.tar.gz"
	// DefaultSchemaLocation is the default location to store downloaded schemas.
	DefaultSchemaLocation = "/tmp/flux-crd-schemas/master-standalone-strict"
)

// Client provides kubeconform validation functionality.
type Client struct {
	schemaLocation string
}

// NewClient creates a new kubeconform client.
func NewClient() *Client {
	return &Client{
		schemaLocation: DefaultSchemaLocation,
	}
}

// NewClientWithSchemaLocation creates a new kubeconform client with a custom schema location.
func NewClientWithSchemaLocation(schemaLocation string) *Client {
	return &Client{
		schemaLocation: schemaLocation,
	}
}

// DownloadFluxSchemas downloads the Flux OpenAPI schemas to the configured location.
func (c *Client) DownloadFluxSchemas(ctx context.Context) error {
	// Create the schema directory
	err := os.MkdirAll(c.schemaLocation, 0o755)
	if err != nil {
		return fmt.Errorf("create schema directory: %w", err)
	}

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, FluxCRDSchemasURL, nil)
	if err != nil {
		return fmt.Errorf("create HTTP request: %w", err)
	}

	// Download the schemas
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download schemas: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download schemas: unexpected status code %d", resp.StatusCode)
	}

	// Extract the tar.gz file
	err = c.extractTarGz(resp.Body, c.schemaLocation)
	if err != nil {
		return fmt.Errorf("extract schemas: %w", err)
	}

	return nil
}

// ValidateFile validates a single Kubernetes manifest file.
func (c *Client) ValidateFile(ctx context.Context, filePath string, opts *ValidationOptions) error {
	if opts == nil {
		opts = &ValidationOptions{}
	}

	args := c.buildArgs(opts)
	args = append(args, filePath)

	cmd := exec.CommandContext(ctx, "kubeconform", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("validate file %s: %w\n%s", filePath, err, string(output))
	}

	return nil
}

// ValidateManifests validates Kubernetes manifests from a reader (e.g., kustomize build output).
func (c *Client) ValidateManifests(ctx context.Context, reader io.Reader, opts *ValidationOptions) error {
	if opts == nil {
		opts = &ValidationOptions{}
	}

	args := c.buildArgs(opts)

	cmd := exec.CommandContext(ctx, "kubeconform", args...)
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
	args = append(args, "-schema-location", c.schemaLocation)

	return args
}

// extractTarGz extracts a tar.gz stream to the specified destination.
func (c *Client) extractTarGz(reader io.Reader, dest string) error {
	gzr, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar header: %w", err)
		}

		// Sanitize the file path to prevent path traversal
		target := filepath.Join(dest, header.Name)
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(target, 0o755)
			if err != nil {
				return fmt.Errorf("create directory %s: %w", target, err)
			}
		case tar.TypeReg:
			err = c.extractFile(tr, target, header.Mode)
			if err != nil {
				return fmt.Errorf("extract file %s: %w", target, err)
			}
		}
	}

	return nil
}

// extractFile extracts a single file from a tar archive.
func (c *Client) extractFile(tr *tar.Reader, target string, mode int64) (err error) {
	// Ensure the directory exists
	err = os.MkdirAll(filepath.Dir(target), 0o755)
	if err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(mode))
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close file: %w", cerr)
		}
	}()

	_, err = io.Copy(file, tr)
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return
}
