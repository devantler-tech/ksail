package workload_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload"
	"github.com/gkampitakis/go-snaps/snaps"
)

const validNamespaceManifest = `apiVersion: v1
kind: Namespace
metadata:
  name: test-namespace
`

func TestNewValidateCmdHasCorrectDefaults(t *testing.T) {
	t.Parallel()

	cmd := workload.NewValidateCmd()

	if cmd.Use != "validate [PATH]" {
		t.Fatalf("expected Use to be 'validate [PATH]', got %q", cmd.Use)
	}

	if cmd.Short != "Validate Kubernetes manifests and kustomizations" {
		t.Fatalf(
			"expected Short description to be 'Validate Kubernetes manifests and kustomizations', got %q",
			cmd.Short,
		)
	}

	// Check default flag values
	skipSecrets, _ := cmd.Flags().GetBool("skip-secrets")
	if !skipSecrets {
		t.Fatal("expected skip-secrets to default to true")
	}

	strict, _ := cmd.Flags().GetBool("strict")
	if !strict {
		t.Fatal("expected strict to default to true")
	}

	ignoreMissingSchemas, _ := cmd.Flags().GetBool("ignore-missing-schemas")
	if !ignoreMissingSchemas {
		t.Fatal("expected ignore-missing-schemas to default to true")
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	if verbose {
		t.Fatal("expected verbose to default to false")
	}
}

func TestValidateCmdShowsHelp(t *testing.T) {
	t.Parallel()

	cmd := workload.NewValidateCmd()

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	snaps.MatchSnapshot(t, output.String())
}

func TestValidateCmdRejectsMultiplePaths(t *testing.T) {
	t.Parallel()

	cmd := workload.NewValidateCmd()

	// This test validates that the command rejects multiple path arguments
	cmd.SetArgs([]string{
		"/some/path1",
		"/some/path2",
	})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cmd.Execute()
	// We expect an error because multiple paths are not allowed
	if err == nil {
		t.Fatal("expected error for multiple paths")
	}

	// The error should be about too many arguments
	if !strings.Contains(err.Error(), "accepts at most 1 arg(s)") {
		t.Fatalf("expected error about too many arguments, got %v", err)
	}
}

func TestValidateCmdAcceptsSinglePath(t *testing.T) {
	t.Parallel()

	cmd := workload.NewValidateCmd()

	// This test validates that the command accepts a single path argument
	// It will fail during execution because the path doesn't exist, but that's expected
	cmd.SetArgs([]string{
		"/nonexistent/path",
	})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cmd.Execute()
	// We expect an error because the path doesn't exist
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}

	// The error should be about path access, not argument parsing
	if !strings.Contains(err.Error(), "access path") {
		t.Fatalf("expected error about path access, got %v", err)
	}
}

func TestValidateCmdWithValidManifest(t *testing.T) {
	t.Parallel()

	// Create a temporary directory with a valid manifest
	tmpDir := t.TempDir()

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

	cmd := workload.NewValidateCmd()
	cmd.SetArgs([]string{tmpDir})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("expected validation to succeed, got error: %v", err)
	}
}

func TestValidateCmdWithInvalidManifest(t *testing.T) {
	t.Parallel()

	// Create a temporary directory with an invalid manifest
	tmpDir := t.TempDir()

	// Create an invalid manifest
	invalidManifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data: "invalid structure"
`
	manifestPath := filepath.Join(tmpDir, "invalid-manifest.yaml")

	err := os.WriteFile(manifestPath, []byte(invalidManifest), 0o600)
	if err != nil {
		t.Fatalf("failed to write test manifest: %v", err)
	}

	cmd := workload.NewValidateCmd()
	cmd.SetArgs([]string{tmpDir})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected validation to fail for invalid manifest")
	}
}

func TestValidateCmdWithKustomization(t *testing.T) {
	t.Parallel()

	// Create a temporary directory with a valid kustomization
	tmpDir := t.TempDir()

	// Create a simple ConfigMap
	configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: default
data:
  key: value
`

	err := os.WriteFile(filepath.Join(tmpDir, "configmap.yaml"), []byte(configMapYAML), 0o600)
	if err != nil {
		t.Fatalf("failed to write configmap: %v", err)
	}

	// Create a kustomization.yaml
	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - configmap.yaml
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(kustomizationYAML),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write kustomization: %v", err)
	}

	cmd := workload.NewValidateCmd()
	cmd.SetArgs([]string{tmpDir})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("expected validation to succeed, got error: %v", err)
	}
}

func TestValidateCmdWithSkipSecretsFlag(t *testing.T) {
	t.Parallel()

	// Create a temporary directory with a Secret
	tmpDir := t.TempDir()

	// Create a Secret (which may have SOPS fields that could fail validation)
	//nolint:gosec // G101: This is a test secret manifest, not a hardcoded credential
	secretYAML := `apiVersion: v1
kind: Secret
metadata:
  name: test-secret
  namespace: default
type: Opaque
data:
  key: dmFsdWU=
sops:
  # SOPS metadata that would fail validation without skip-secrets
  encrypted_regex: ^(data|stringData)$
`

	err := os.WriteFile(filepath.Join(tmpDir, "secret.yaml"), []byte(secretYAML), 0o600)
	if err != nil {
		t.Fatalf("failed to write secret: %v", err)
	}

	cmd := workload.NewValidateCmd()
	cmd.SetArgs([]string{
		"--skip-secrets=true",
		tmpDir,
	})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("expected validation with skip-secrets to succeed, got error: %v", err)
	}
}

func TestValidateCmdWithVerboseFlag(t *testing.T) {
	t.Parallel()

	// Create a temporary directory with a valid manifest
	tmpDir := t.TempDir()

	manifestPath := filepath.Join(tmpDir, "namespace.yaml")

	err := os.WriteFile(manifestPath, []byte(validNamespaceManifest), 0o600)
	if err != nil {
		t.Fatalf("failed to write test manifest: %v", err)
	}

	cmd := workload.NewValidateCmd()
	cmd.SetArgs([]string{
		"--verbose",
		tmpDir,
	})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("expected validation to succeed, got error: %v", err)
	}

	// Note: We can't easily check for verbose output without capturing stdout/stderr
	// but we can verify the command ran successfully with the flag
}

//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() - they are incompatible
func TestValidateCmdWithDefaultPath(t *testing.T) {
	// Note: Cannot use t.Parallel() here because we use t.Chdir()

	// Create a temporary directory with a valid manifest and change to it
	tmpDir := t.TempDir()

	manifestPath := filepath.Join(tmpDir, "namespace.yaml")

	err := os.WriteFile(manifestPath, []byte(validNamespaceManifest), 0o600)
	if err != nil {
		t.Fatalf("failed to write test manifest: %v", err)
	}

	// Use t.Chdir to change directory (automatically reverts after test)
	t.Chdir(tmpDir)

	// Run validate without path argument (should use current directory)
	cmd := workload.NewValidateCmd()
	cmd.SetArgs([]string{})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("expected validation to succeed with default path, got error: %v", err)
	}
}

func TestValidateCmdWithEmptyDirectory(t *testing.T) {
	t.Parallel()

	// Create an empty temporary directory
	tmpDir := t.TempDir()

	cmd := workload.NewValidateCmd()
	cmd.SetArgs([]string{tmpDir})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	// Empty directory should succeed (no files to validate)
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected validation of empty directory to succeed, got error: %v", err)
	}
}

func TestValidateCmdWithMixedValidAndInvalidFiles(t *testing.T) {
	t.Parallel()

	// Create a temporary directory with both valid and invalid manifests
	tmpDir := t.TempDir()

	// Valid manifest
	validManifest := `apiVersion: v1
kind: Namespace
metadata:
  name: test-namespace
`

	err := os.WriteFile(
		filepath.Join(tmpDir, "valid.yaml"),
		[]byte(validManifest),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write valid manifest: %v", err)
	}

	// Invalid manifest
	invalidManifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data: "invalid"
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "invalid.yaml"),
		[]byte(invalidManifest),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write invalid manifest: %v", err)
	}

	cmd := workload.NewValidateCmd()
	cmd.SetArgs([]string{tmpDir})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err = cmd.Execute()
	// Should fail because one file is invalid
	if err == nil {
		t.Fatal("expected validation to fail when directory contains invalid files")
	}
}

func setupValidManifestDir(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "namespace.yaml")

	err := os.WriteFile(manifestPath, []byte(validNamespaceManifest), 0o600)
	if err != nil {
		t.Fatalf("failed to write test manifest: %v", err)
	}

	return tmpDir
}

func TestValidateCmdFlagCombinations(t *testing.T) {
	t.Parallel()

	tmpDir := setupValidManifestDir(t)

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "strict enabled",
			args: []string{"--strict=true", tmpDir},
		},
		{
			name: "strict disabled",
			args: []string{"--strict=false", tmpDir},
		},
		{
			name: "ignore-missing-schemas enabled",
			args: []string{"--ignore-missing-schemas=true", tmpDir},
		},
		{
			name: "ignore-missing-schemas disabled",
			args: []string{"--ignore-missing-schemas=false", tmpDir},
		},
		{
			name: "all flags",
			args: []string{
				"--skip-secrets=true",
				"--strict=true",
				"--ignore-missing-schemas=true",
				"--verbose",
				tmpDir,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := workload.NewValidateCmd()
			cmd.SetArgs(testCase.args)

			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)

			err := cmd.Execute()
			if err != nil {
				t.Fatalf(
					"expected validation to succeed with %s, got error: %v",
					testCase.name,
					err,
				)
			}
		})
	}
}
