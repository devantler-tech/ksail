package kubeconform_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
)

const validNamespaceYAML = `apiVersion: v1
kind: Namespace
metadata:
  name: test-namespace
`

//nolint:gosec // G101: test manifest, not a hardcoded credential
const testSecretManifest = `apiVersion: v1
kind: Secret
metadata:
  name: test-secret
  namespace: default
type: Opaque
data:
  key: dmFsdWU=
`

func TestNewClient(t *testing.T) {
	t.Parallel()

	client := kubeconform.NewClient()

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestValidationOptions(t *testing.T) {
	t.Parallel()

	opts := &kubeconform.ValidationOptions{
		SkipKinds:            []string{"Secret", "ConfigMap"},
		Strict:               true,
		IgnoreMissingSchemas: true,
	}

	if len(opts.SkipKinds) != 2 {
		t.Fatalf("expected 2 skip kinds, got %d", len(opts.SkipKinds))
	}

	if opts.SkipKinds[0] != "Secret" {
		t.Fatalf("expected first skip kind to be Secret, got %s", opts.SkipKinds[0])
	}

	if !opts.Strict {
		t.Fatal("expected Strict to be true")
	}

	if !opts.IgnoreMissingSchemas {
		t.Fatal("expected IgnoreMissingSchemas to be true")
	}
}

func TestValidateFile_ValidManifest(t *testing.T) {
	t.Parallel()

	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	// Create a valid Kubernetes manifest
	validManifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: default
data:
  key: value
`
	manifestPath := filepath.Join(tmpDir, "valid-manifest.yaml")

	err := os.WriteFile(manifestPath, []byte(validManifest), 0o600)
	if err != nil {
		t.Fatalf("failed to write test manifest: %v", err)
	}

	// Test validation
	client := kubeconform.NewClient()
	opts := &kubeconform.ValidationOptions{
		Strict:               true,
		IgnoreMissingSchemas: true,
	}

	ctx := context.Background()

	err = client.ValidateFile(ctx, manifestPath, opts)
	if err != nil {
		t.Fatalf("expected valid manifest to pass validation, got error: %v", err)
	}
}

func TestValidateFile_InvalidManifest(t *testing.T) {
	t.Parallel()

	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	// Create an invalid Kubernetes manifest (missing required fields)
	invalidManifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
# Missing required namespace field in strict mode
data: invalid
`
	manifestPath := filepath.Join(tmpDir, "invalid-manifest.yaml")

	err := os.WriteFile(manifestPath, []byte(invalidManifest), 0o600)
	if err != nil {
		t.Fatalf("failed to write test manifest: %v", err)
	}

	// Test validation
	client := kubeconform.NewClient()
	opts := &kubeconform.ValidationOptions{
		Strict:               true,
		IgnoreMissingSchemas: true,
	}

	ctx := context.Background()

	err = client.ValidateFile(ctx, manifestPath, opts)
	if err == nil {
		t.Fatal("expected invalid manifest to fail validation")
	}

	// Check that it's a validation error
	if !strings.Contains(err.Error(), "validation failed") {
		t.Fatalf("expected validation error, got: %v", err)
	}
}

func TestValidateFile_NonExistentFile(t *testing.T) {
	t.Parallel()

	client := kubeconform.NewClient()
	opts := &kubeconform.ValidationOptions{
		Strict:               true,
		IgnoreMissingSchemas: true,
	}

	ctx := context.Background()

	err := client.ValidateFile(ctx, "/nonexistent/file.yaml", opts)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}

	// Check that it's a file open error
	if !strings.Contains(err.Error(), "open file") {
		t.Fatalf("expected file open error, got: %v", err)
	}
}

func TestValidateFile_SkipKinds(t *testing.T) {
	t.Parallel()

	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	// Create a Secret manifest (which we'll skip)
	secretManifest := testSecretManifest
	manifestPath := filepath.Join(tmpDir, "secret.yaml")

	err := os.WriteFile(manifestPath, []byte(secretManifest), 0o600)
	if err != nil {
		t.Fatalf("failed to write test manifest: %v", err)
	}

	// Test validation with Secret skipped
	client := kubeconform.NewClient()
	opts := &kubeconform.ValidationOptions{
		SkipKinds:            []string{"Secret"},
		Strict:               true,
		IgnoreMissingSchemas: true,
	}

	ctx := context.Background()

	err = client.ValidateFile(ctx, manifestPath, opts)
	// Should succeed because Secret is skipped
	if err != nil {
		t.Fatalf("expected skipped Secret to pass validation, got error: %v", err)
	}
}

func TestValidateManifests_ValidYAML(t *testing.T) {
	t.Parallel()

	// Create valid YAML content
	validYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: test-namespace
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: test-namespace
data:
  key: value
`

	client := kubeconform.NewClient()
	opts := &kubeconform.ValidationOptions{
		Strict:               true,
		IgnoreMissingSchemas: true,
	}

	ctx := context.Background()
	reader := bytes.NewReader([]byte(validYAML))

	err := client.ValidateManifests(ctx, reader, opts)
	if err != nil {
		t.Fatalf("expected valid YAML to pass validation, got error: %v", err)
	}
}

func TestValidateManifests_InvalidYAML(t *testing.T) {
	t.Parallel()

	// Create invalid YAML content
	invalidYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data: "this is not valid"
`

	client := kubeconform.NewClient()
	opts := &kubeconform.ValidationOptions{
		Strict:               true,
		IgnoreMissingSchemas: true,
	}

	ctx := context.Background()
	reader := bytes.NewReader([]byte(invalidYAML))

	err := client.ValidateManifests(ctx, reader, opts)
	if err == nil {
		t.Fatal("expected invalid YAML to fail validation")
	}
	// Check that it's a validation error
	if !strings.Contains(err.Error(), "validation failed") {
		t.Fatalf("expected validation error, got: %v", err)
	}
}

func TestValidateBytes_ValidYAML(t *testing.T) {
	t.Parallel()

	validYAML := validNamespaceYAML

	client := kubeconform.NewClient()
	opts := &kubeconform.ValidationOptions{
		Strict:               true,
		IgnoreMissingSchemas: true,
	}

	err := client.ValidateBytes(context.Background(), "test.yaml", []byte(validYAML), opts)
	if err != nil {
		t.Fatalf("expected valid YAML to pass validation, got error: %v", err)
	}
}

func TestValidateManifests_NilOptions(t *testing.T) {
	t.Parallel()

	// Test that nil options are handled gracefully
	validYAML := validNamespaceYAML

	client := kubeconform.NewClient()

	ctx := context.Background()
	reader := bytes.NewReader([]byte(validYAML))

	err := client.ValidateManifests(ctx, reader, nil)
	if err != nil {
		t.Fatalf("expected validation with nil options to succeed, got error: %v", err)
	}
}

func TestValidateFile_NilOptions(t *testing.T) {
	t.Parallel()

	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	// Create a valid Kubernetes manifest
	validManifest := validNamespaceYAML
	manifestPath := filepath.Join(tmpDir, "valid-manifest.yaml")

	err := os.WriteFile(manifestPath, []byte(validManifest), 0o600)
	if err != nil {
		t.Fatalf("failed to write test manifest: %v", err)
	}

	// Test validation with nil options
	client := kubeconform.NewClient()

	ctx := context.Background()

	err = client.ValidateFile(ctx, manifestPath, nil)
	if err != nil {
		t.Fatalf("expected validation with nil options to succeed, got error: %v", err)
	}
}

// TestValidateBytes_NamesFailingResource verifies that when a multi-document stream
// contains a schema-invalid resource, the failure message identifies that resource by
// Kind/Namespace/Name — so a finding in a large Kustomize+Helm render is traceable to a
// specific object — without implicating the valid resources in the same stream.
func TestValidateBytes_NamesFailingResource(t *testing.T) {
	t.Parallel()

	// A stream with a valid Namespace and a schema-invalid ConfigMap (data must be a
	// map of strings, not a scalar).
	invalidConfigMap := `apiVersion: v1
kind: ConfigMap
metadata:
  name: broken-config
  namespace: demo
data: "this is not a map"
`
	manifests := validNamespaceYAML + "---\n" + invalidConfigMap

	client := kubeconform.NewClient()
	opts := &kubeconform.ValidationOptions{
		Strict:               true,
		IgnoreMissingSchemas: true,
	}

	err := client.ValidateBytes(context.Background(), "render.yaml", []byte(manifests), opts)
	if err == nil {
		t.Fatal("expected the invalid ConfigMap to fail validation")
	}

	msg := err.Error()
	if !strings.Contains(msg, "ConfigMap/demo/broken-config") {
		t.Fatalf("expected error to name the failing resource, got: %v", err)
	}

	if strings.Contains(msg, "test-namespace") {
		t.Fatalf("expected error not to implicate the valid Namespace, got: %v", err)
	}
}

// TestSplitDocumentsForValidationCopiesLargeMultiDocumentStream verifies that
// split documents are stable copies rather than views into the source buffer.
func TestSplitDocumentsForValidationCopiesLargeMultiDocumentStream(t *testing.T) {
	t.Parallel()

	const (
		documentCount = 512
		payloadSize   = 10 * 1024
	)

	var stream strings.Builder
	for i := range documentCount {
		_, _ = fmt.Fprintf(
			&stream,
			"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm-%03d\ndata:\n  payload: %q\n---\n",
			i,
			strings.Repeat("x", payloadSize),
		)
	}

	source := []byte(stream.String())

	documents, err := kubeconform.SplitDocumentsForValidation(source)
	if err != nil {
		t.Fatalf("expected large stream to split cleanly, got: %v", err)
	}

	if len(documents) != documentCount {
		t.Fatalf("expected %d documents, got %d", documentCount, len(documents))
	}

	for index, document := range documents {
		// Prove each split document owns stable bytes: callers may retry
		// validation over the split output while the original input buffer is
		// no longer trusted.
		source[0] = '#'

		expectedName := fmt.Sprintf("name: cm-%03d", index)
		if !bytes.Contains(document, []byte(expectedName)) {
			t.Fatalf(
				"document %d lost its identity; expected %q in:\n%s",
				index,
				expectedName,
				document,
			)
		}
	}
}

// TestSplitDocumentsForValidationRejectsMalformedYAML verifies malformed
// documents fail while splitting, before any kubeconform validation runs.
func TestSplitDocumentsForValidationRejectsMalformedYAML(t *testing.T) {
	t.Parallel()

	malformedStream := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: broken
--- invalid separator content
`)

	documents, err := kubeconform.SplitDocumentsForValidation(malformedStream)
	if err == nil {
		t.Fatalf("expected malformed YAML to fail splitting, got %d documents", len(documents))
	}

	if !strings.Contains(err.Error(), "read YAML document") {
		t.Fatalf("expected split error to wrap YAML read failure, got: %v", err)
	}
}

// TestValidateBytesStopsBetweenDocumentsWhenContextCancelled verifies that a
// cancelled batch stops before validating later split documents.
func TestValidateBytesStopsBetweenDocumentsWhenContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var schemaRequests atomic.Int32

	schemaServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			schemaRequests.Add(1)
			cancel()

			_, _ = w.Write([]byte(widgetCRDSchema))
		}),
	)
	t.Cleanup(schemaServer.Close)

	client := kubeconform.NewClient()
	opts := &kubeconform.ValidationOptions{
		IgnoreMissingSchemas: false,
		SchemaLocations: []string{
			schemaServer.URL + "/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json",
		},
	}

	err := client.ValidateBytes(
		ctx,
		"widgets.yaml",
		[]byte(validWidgetCR+"---\n"+validWidgetCR),
		opts,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation between documents, got: %v", err)
	}

	if got := schemaRequests.Load(); got == 0 {
		t.Fatal("expected validation to request at least one schema before cancellation")
	}
}

// widgetCRD is a JSON schema for a fictional CRD kind that exists in neither the
// built-in Kubernetes schemas nor the CRDs-catalog, so it can only be validated
// via a caller-supplied schema location.
const widgetCRDSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "apiVersion": {"type": "string"},
    "kind": {"type": "string"},
    "metadata": {"type": "object"},
    "spec": {
      "type": "object",
      "properties": {"size": {"type": "integer"}},
      "required": ["size"],
      "additionalProperties": false
    }
  },
  "required": ["spec"]
}`

const validWidgetCR = `apiVersion: example.com/v1
kind: WidgetThing
metadata:
  name: widget
spec:
  size: 3
`

// invalidWidgetCR violates widgetCRDSchema (additionalProperties: false).
const invalidWidgetCR = `apiVersion: example.com/v1
kind: WidgetThing
metadata:
  name: widget
spec:
  size: 3
  bogus: true
`

// writeWidgetSchema writes widgetCRDSchema where kubeconform's template resolves
// it (lowercased kind + version) and returns the template schema location.
func writeWidgetSchema(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	err := os.WriteFile(
		filepath.Join(tmpDir, "widgetthing_v1.json"),
		[]byte(widgetCRDSchema),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write widget schema: %v", err)
	}

	return filepath.Join(tmpDir, "{{.ResourceKind}}_{{.ResourceAPIVersion}}.json")
}

func TestValidateBytes_SchemaLocation_ValidatesCatalogAbsentCRD(t *testing.T) {
	t.Parallel()

	schemaLoc := writeWidgetSchema(t)
	client := kubeconform.NewClient()
	ctx := context.Background()

	// With the supplied schema location, a valid CR passes...
	err := client.ValidateBytes(
		ctx,
		"widget.yaml",
		[]byte(validWidgetCR),
		&kubeconform.ValidationOptions{
			IgnoreMissingSchemas: false,
			SchemaLocations:      []string{schemaLoc},
		},
	)
	if err != nil {
		t.Fatalf("expected valid widget CR to pass with schema location, got: %v", err)
	}

	// ...and a CR that violates the supplied schema is caught.
	err = client.ValidateBytes(
		ctx,
		"widget.yaml",
		[]byte(invalidWidgetCR),
		&kubeconform.ValidationOptions{
			IgnoreMissingSchemas: false,
			SchemaLocations:      []string{schemaLoc},
		},
	)
	if err == nil {
		t.Fatal("expected invalid widget CR to fail validation against the supplied schema")
	}
}

func TestValidateBytes_SchemaLocation_AbsentMeansKindIgnored(t *testing.T) {
	t.Parallel()

	client := kubeconform.NewClient()
	ctx := context.Background()

	// Without the schema location the kind has no schema; IgnoreMissingSchemas
	// lets the otherwise-invalid CR pass — proving the schema location is what
	// enables the catch in the test above (and is appended, not relied upon).
	err := client.ValidateBytes(
		ctx,
		"widget.yaml",
		[]byte(invalidWidgetCR),
		&kubeconform.ValidationOptions{
			IgnoreMissingSchemas: true,
		},
	)
	if err != nil {
		t.Fatalf("expected catalog-absent CR to be ignored without a schema location, got: %v", err)
	}
}
