package kubeconform

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/netretry"
	"github.com/yannh/kubeconform/pkg/validator"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
)

// ErrValidationFailed indicates that validation failed.
var ErrValidationFailed = errors.New("validation failed")

const (
	// schemaCacheDirPerm is the permission for the schema cache directory.
	schemaCacheDirPerm = 0o700

	// builtinSchemaLocationCount is the number of built-in schema locations
	// (the default Kubernetes schemas and the CRDs-catalog) prepended before any
	// caller-supplied locations.
	builtinSchemaLocationCount = 2

	// defaultValidateMaxRetryAttempts bounds how many times a validation is
	// retried when a schema download hits a transient network error.
	defaultValidateMaxRetryAttempts = 3
	// defaultValidateRetryBaseWait is the initial backoff between retries.
	defaultValidateRetryBaseWait = 500 * time.Millisecond
	// defaultValidateRetryMaxWait caps the backoff between retries.
	defaultValidateRetryMaxWait = 5 * time.Second
)

// Client provides kubeconform validation functionality.
type Client struct {
	// maxRetryAttempts bounds retries for transient schema-download failures.
	maxRetryAttempts int
	// retryBaseWait is the initial exponential-backoff delay between retries.
	retryBaseWait time.Duration
	// retryMaxWait caps the exponential-backoff delay between retries.
	retryMaxWait time.Duration
}

// NewClient creates a new kubeconform client.
func NewClient() *Client {
	return &Client{
		maxRetryAttempts: defaultValidateMaxRetryAttempts,
		retryBaseWait:    defaultValidateRetryBaseWait,
		retryMaxWait:     defaultValidateRetryMaxWait,
	}
}

// ValidateFile validates a single Kubernetes manifest file.
func (c *Client) ValidateFile(ctx context.Context, filePath string, opts *ValidationOptions) error {
	// Check context before starting so a cancelled context is reported before any I/O.
	if ctx.Err() != nil {
		return fmt.Errorf("%w", ctx.Err())
	}

	// Read the whole file up front so a transient schema-download failure can be retried
	// without re-opening the file.
	data, err := os.ReadFile(filePath) //nolint:gosec // filePath is provided by the caller
	if err != nil {
		return fmt.Errorf("open file %s: %w", filePath, err)
	}

	return c.validateData(ctx, filePath, data, opts)
}

// ValidateBytes validates Kubernetes manifests from raw bytes while preserving the source name.
func (c *Client) ValidateBytes(
	ctx context.Context,
	sourceName string,
	data []byte,
	opts *ValidationOptions,
) error {
	return c.validateData(ctx, sourceName, data, opts)
}

// ValidateManifests validates Kubernetes manifests from a reader (e.g., kustomize build output).
func (c *Client) ValidateManifests(
	ctx context.Context,
	reader io.Reader,
	opts *ValidationOptions,
) error {
	// Check context before starting so a cancelled context is reported before any I/O.
	if ctx.Err() != nil {
		return fmt.Errorf("%w", ctx.Err())
	}

	// Buffer the reader so a transient schema-download failure can be retried.
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read manifests: %w", err)
	}

	return c.validateData(ctx, "stdin", data, opts)
}

// validateData runs kubeconform validation over data, retrying transient
// schema-download failures. It is the shared core of the public Validate* methods.
func (c *Client) validateData(
	ctx context.Context,
	sourceName string,
	data []byte,
	opts *ValidationOptions,
) error {
	if opts == nil {
		opts = &ValidationOptions{}
	}

	if ctx.Err() != nil {
		return fmt.Errorf("%w", ctx.Err())
	}

	documents, err := splitDocumentsForValidation(data)
	if err != nil {
		return fmt.Errorf("split manifests: %w", err)
	}

	kubeValidator, err := c.createValidator(opts)
	if err != nil {
		return fmt.Errorf("create validator: %w", err)
	}

	return c.validateWithRetry(ctx, func() error {
		results := make([]validator.Result, 0, len(documents))
		for _, document := range documents {
			results = append(
				results,
				kubeValidator.Validate(sourceName, io.NopCloser(bytes.NewReader(document)))...,
			)
		}

		return c.processResults(results, opts.Attribution)
	})
}

func splitDocumentsForValidation(data []byte) ([][]byte, error) {
	reader := k8syaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	documents := [][]byte{}

	for {
		document, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("read YAML document: %w", err)
		}

		if len(bytes.TrimSpace(document)) == 0 {
			continue
		}

		documents = append(documents, bytes.Clone(document))
	}

	return documents, nil
}

// validateWithRetry runs validate up to c.maxRetryAttempts times, retrying only
// when the error is a transient network error per netretry.IsRetryable.
// kubeconform downloads JSON schemas from a remote registry, and those fetches
// can fail with transient errors such as "unexpected EOF" that would otherwise
// red an entire validation run. Successfully-downloaded schemas are cached, so a
// retry re-fetches only what failed. The returned error is left unwrapped to
// preserve ErrValidationFailed identity for callers using errors.Is.
func (c *Client) validateWithRetry(ctx context.Context, validate func() error) error {
	// netretry.Do returns the last validate() error unwrapped on exhaustion,
	// preserving ErrValidationFailed identity for callers using errors.Is, and
	// the context error (cancellation-tagged) when the backoff wait is cancelled.
	return netretry.Do( //nolint:wrapcheck // identity preserved
		ctx,
		max(c.maxRetryAttempts, 1),
		c.retryBaseWait,
		c.retryMaxWait,
		validate,
	)
}

// processResults processes validation results and returns an error if validation failed.
// Error details are included in the returned error message (not written to stderr)
// to avoid interleaving with ProgressGroup's ANSI output.
// Each failing resource's identity (Kind/Namespace/Name) is prefixed to its detail so
// that, when validating a stream of many manifests (e.g. a whole Kustomize + Helm render),
// the message points at the offending resource instead of surfacing a bare schema error.
// The caller is responsible for including any file/source context in the wrapping error.
// attribution optionally maps a resource identity to a source descriptor so a failure
// can be traced to the layer that produced it (see formatFailure); a nil map is fine.
func (c *Client) processResults(results []validator.Result, attribution map[string]string) error {
	var errDetails []string

	for _, res := range results {
		if res.Status == validator.Invalid || res.Status == validator.Error {
			errDetails = append(errDetails, formatFailure(res, attribution))
		}
	}

	if len(errDetails) > 0 {
		return fmt.Errorf("%w: %s", ErrValidationFailed, strings.Join(errDetails, "; "))
	}

	return nil
}

// formatFailure renders a single failing result as "<identity>: <detail>", falling
// back to just the detail when the resource signature is unavailable (e.g. a document
// missing apiVersion/kind). When attribution carries an entry for the resource's
// identity, its source descriptor is appended as " (from <source>)" so a failure in a
// multi-layer render is traced to the layer that produced it. Preserving the original
// detail keeps existing error substrings intact for callers that match on them.
func formatFailure(res validator.Result, attribution map[string]string) string {
	detail := fmt.Sprintf("%v", res.Err)

	identity := resourceIdentity(res)
	if identity == "" {
		return detail
	}

	message := identity + ": " + detail

	if source := attribution[identity]; source != "" {
		message += " (from " + source + ")"
	}

	return message
}

// resourceIdentity returns a "<Kind>/<Namespace>/<Name>" (or "<Kind>/<Name>" for
// cluster-scoped resources) label for a validation result, or "" when the result
// carries no usable signature.
func resourceIdentity(res validator.Result) string {
	sig, err := res.Resource.Signature()
	if err != nil || sig == nil || sig.Kind == "" {
		return ""
	}

	if sig.Namespace != "" {
		return fmt.Sprintf("%s/%s/%s", sig.Kind, sig.Namespace, sig.Name)
	}

	return fmt.Sprintf("%s/%s", sig.Kind, sig.Name)
}

// ValidationOptions configures validation behavior.
type ValidationOptions struct {
	// SkipKinds is a list of Kubernetes kinds to skip during validation (e.g., "Secret").
	SkipKinds []string
	// SchemaLocations lists additional kubeconform schema locations (local directories
	// or URL templates) appended after the built-in Kubernetes schemas and the
	// CRDs-catalog. They let a repo validate CRDs that are absent from (or stale in)
	// the catalog against a supplied schema instead of skipping the kind entirely.
	SchemaLocations []string
	// Strict enables strict validation mode.
	Strict bool
	// IgnoreMissingSchemas ignores resources with missing schemas.
	IgnoreMissingSchemas bool
	// Attribution optionally maps a resource identity ("Kind/Namespace/Name", or
	// "Kind/Name" for cluster-scoped resources) to a human-readable source descriptor
	// (e.g. "HelmRelease flux-system/app"). When a failing resource's identity is
	// present, its source is appended to the failure message so a validation error in
	// a multi-layer render points at the originating layer. A nil map (the default)
	// disables attribution and leaves failure messages unchanged.
	Attribution map[string]string
}

// createValidator creates a kubeconform validator with the given options.
func (c *Client) createValidator(opts *ValidationOptions) (validator.Validator, error) {
	// Create schema locations. Caller-supplied locations are appended last so they
	// act as a fallback for CRDs absent from the catalog (the catalog URL 404s and
	// kubeconform falls through to these) without changing precedence for kinds
	// already covered.
	schemaLocations := make([]string, 0, builtinSchemaLocationCount+len(opts.SchemaLocations))
	schemaLocations = append(schemaLocations,
		// Default Kubernetes schemas
		"default",
		// Add Datree CRDs catalog for additional CRD schemas
		"https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/"+
			"{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json",
	)
	schemaLocations = append(schemaLocations, opts.SchemaLocations...)

	// Convert skip kinds to map
	skipKinds := make(map[string]struct{})
	for _, kind := range opts.SkipKinds {
		skipKinds[kind] = struct{}{}
	}

	// Set up schema cache directory
	var cacheDir string

	userCacheDir, userCacheDirErr := os.UserCacheDir()
	if userCacheDirErr == nil {
		cacheDir = filepath.Join(userCacheDir, "ksail", "kubeconform")

		err := os.MkdirAll(cacheDir, schemaCacheDirPerm)
		if err != nil {
			return nil, fmt.Errorf("create schema cache directory: %w", err)
		}
	} else {
		// Fallback: use a stable cache directory under the system temp dir to avoid leaking
		// unique directories on every validator construction when $HOME is unavailable (e.g. CI).
		cacheDir = filepath.Join(os.TempDir(), "ksail", "kubeconform")

		err := os.MkdirAll(cacheDir, schemaCacheDirPerm)
		if err != nil {
			return nil, fmt.Errorf("create fallback schema cache directory: %w", err)
		}
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
