package kubeconform_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/pkg/client/kubeconform"
)

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
		Verbose:              false,
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

	if opts.Verbose {
		t.Fatal("expected Verbose to be false")
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
		Verbose:              false,
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
		Verbose:              false,
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
		Verbose:              false,
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
	//nolint:gosec // G101: This is a test secret manifest, not a hardcoded credential
	secretManifest := `apiVersion: v1
kind: Secret
metadata:
  name: test-secret
  namespace: default
type: Opaque
data:
  key: dmFsdWU=
`
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
		Verbose:              false,
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
		Verbose:              false,
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
		Verbose:              false,
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

func TestValidateManifests_NilOptions(t *testing.T) {
	t.Parallel()

	// Test that nil options are handled gracefully
	validYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: test-namespace
`

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
	validManifest := `apiVersion: v1
kind: Namespace
metadata:
  name: test-namespace
`
	manifestPath := filepath.Join(tmpDir, "valid-manifest.yaml")

	err := os.WriteFile(manifestPath, []byte(validManifest), 0o600)
	if err != nil {
		t.Fatalf("failed to write test manifest: %v", err)
	}

	// Test validation with nil options
	client := kubeconform.NewClient()

	ctx := context.Background()
	err := client.ValidateFile(ctx, manifestPath, nil)
	if err != nil {
		t.Fatalf("expected validation with nil options to succeed, got error: %v", err)
	}
}
