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
	if strict {
		t.Fatal("expected strict to default to false")
	}

	ignoreMissingSchemas, _ := cmd.Flags().GetBool("ignore-missing-schemas")
	if !ignoreMissingSchemas {
		t.Fatal("expected ignore-missing-schemas to default to true")
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

	// The error should be about path resolution or access, not argument parsing
	if !strings.Contains(err.Error(), "access path") &&
		!strings.Contains(err.Error(), "resolve path") {
		t.Fatalf("expected error about path access or resolution, got %v", err)
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

// setupSourceDirTestDir creates a temporary directory with a custom "manifests" source
// directory, a ksail.yaml pointing to it, a Kind distribution config, and a non-K8s
// YAML at the root to verify that only the source directory is validated.
func setupSourceDirTestDir(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	customDir := filepath.Join(tmpDir, "manifests")

	err := os.MkdirAll(customDir, 0o750)
	if err != nil {
		t.Fatalf("failed to create custom dir: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(customDir, "namespace.yaml"),
		[]byte(validNamespaceManifest), 0o600,
	)
	if err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	ksailConfig := `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    distributionConfig: kind.yaml
  workload:
    sourceDirectory: manifests
`

	err = os.WriteFile(filepath.Join(tmpDir, "ksail.yaml"), []byte(ksailConfig), 0o600)
	if err != nil {
		t.Fatalf("failed to write ksail.yaml: %v", err)
	}

	kindConfig := `apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
name: kind
`

	err = os.WriteFile(filepath.Join(tmpDir, "kind.yaml"), []byte(kindConfig), 0o600)
	if err != nil {
		t.Fatalf("failed to write kind.yaml: %v", err)
	}

	nonK8sYAML := `name: ci
on: push
jobs:
  build:
    runs-on: ubuntu-latest
`

	err = os.WriteFile(filepath.Join(tmpDir, "ci.yaml"), []byte(nonK8sYAML), 0o600)
	if err != nil {
		t.Fatalf("failed to write non-K8s YAML: %v", err)
	}

	return tmpDir
}

//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() - they are incompatible
func TestValidateCmdUsesSourceDirectoryFromConfig(t *testing.T) {
	tmpDir := setupSourceDirTestDir(t)

	t.Chdir(tmpDir)

	cmd := workload.NewValidateCmd()
	cmd.SetArgs([]string{})

	var output bytes.Buffer

	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf(
			"expected validation to succeed using sourceDirectory from ksail.yaml, got error: %v\noutput: %s",
			err,
			output.String(),
		)
	}
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

// setupPatchTestDir creates a temp directory with a valid ConfigMap base resource,
// a patch file (not valid standalone), and a kustomization.yaml with the given content.
func setupPatchTestDir(t *testing.T, patchContent, kustomizationYAML string) string {
	t.Helper()

	tmpDir := t.TempDir()

	baseYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  namespace: default
data:
  key: value
`

	err := os.WriteFile(filepath.Join(tmpDir, "configmap.yaml"), []byte(baseYAML), 0o600)
	if err != nil {
		t.Fatalf("failed to write base manifest: %v", err)
	}

	patchDir := filepath.Join(tmpDir, "patches")

	err = os.MkdirAll(patchDir, 0o750)
	if err != nil {
		t.Fatalf("failed to create patch dir: %v", err)
	}

	err = os.WriteFile(filepath.Join(patchDir, "patch.yaml"), []byte(patchContent), 0o600)
	if err != nil {
		t.Fatalf("failed to write patch manifest: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(kustomizationYAML),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write kustomization.yaml: %v", err)
	}

	return tmpDir
}

type patchTestCase struct {
	name              string
	patchContent      string
	kustomizationYAML string
}

func patchSkipTestCases() []patchTestCase {
	// JSON 6902 patch — an array of ops, not a valid standalone K8s resource.
	json6902Patch := `- op: add
  path: /data/extra-key
  value: extra-value
`

	// Strategic merge patch — valid for kustomize, also valid standalone.
	// Used to exercise the patchesStrategicMerge collection code path.
	smpPatch := `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
data:
  extra-key: extra-value
`

	return []patchTestCase{
		{
			name:         "modern patches field",
			patchContent: json6902Patch,
			kustomizationYAML: `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - configmap.yaml
patches:
  - path: patches/patch.yaml
    target:
      kind: ConfigMap
      name: my-config
`,
		},
		{
			name:         "deprecated patchesStrategicMerge",
			patchContent: smpPatch,
			kustomizationYAML: `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - configmap.yaml
patchesStrategicMerge:
  - patches/patch.yaml
`,
		},
		{
			name:         "deprecated patchesJson6902",
			patchContent: json6902Patch,
			kustomizationYAML: `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - configmap.yaml
patchesJson6902:
  - path: patches/patch.yaml
    target:
      kind: ConfigMap
      version: v1
      name: my-config
`,
		},
	}
}

func TestValidateCmdSkipsKustomizePatches(t *testing.T) {
	t.Parallel()

	for _, tc := range patchSkipTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := setupPatchTestDir(t, tc.patchContent, tc.kustomizationYAML)

			cmd := workload.NewValidateCmd()
			cmd.SetArgs([]string{tmpDir})

			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)

			err := cmd.Execute()
			if err != nil {
				t.Fatalf(
					"expected validation to succeed (patch should be excluded), got error: %v\noutput: %s",
					err,
					output.String(),
				)
			}
		})
	}
}

func TestValidateCmdSubstitutesFluxPostBuildVariables(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	err := os.MkdirAll(filepath.Join(tmpDir, "bases", "apps"), 0o750)
	if err != nil {
		t.Fatalf("failed to create bases/apps dir: %v", err)
	}

	err = os.MkdirAll(filepath.Join(tmpDir, "clusters", "local", "apps"), 0o750)
	if err != nil {
		t.Fatalf("failed to create clusters/local/apps dir: %v", err)
	}

	err = os.MkdirAll(filepath.Join(tmpDir, "clusters", "local", "variables"), 0o750)
	if err != nil {
		t.Fatalf("failed to create clusters/local/variables dir: %v", err)
	}

	basesKustomization := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - deployment.yaml
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "bases", "apps", "kustomization.yaml"),
		[]byte(basesKustomization),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write base kustomization: %v", err)
	}

	deploymentYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: auth-proxy
spec:
  replicas: ${auth_proxy_replicas:=2}
  selector:
    matchLabels:
      app: auth-proxy
  template:
    metadata:
      labels:
        app: auth-proxy
    spec:
      containers:
        - name: auth-proxy
          image: nginx:1.27.1
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "bases", "apps", "deployment.yaml"),
		[]byte(deploymentYAML),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write deployment: %v", err)
	}

	appsFluxKustomization := "apiVersion: kustomize.toolkit.fluxcd.io/v1\n" +
		"kind: Kustomization\n" +
		"metadata:\n" +
		"  name: apps\n" +
		"  namespace: flux-system\n" +
		"spec:\n" +
		"  interval: 60m\n" +
		"  prune: true\n" +
		"  postBuild:\n" +
		"    substituteFrom:\n" +
		"      - kind: ConfigMap\n" +
		"        name: variables-cluster\n" +
		"  sourceRef:\n" +
		"    kind: OCIRepository\n" +
		"    name: flux-system\n" +
		"  path: clusters/local/apps/\n"

	err = os.WriteFile(
		filepath.Join(tmpDir, "clusters", "local", "apps", "flux-kustomization.yaml"),
		[]byte(appsFluxKustomization),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write apps flux kustomization: %v", err)
	}

	clusterAppsKustomization := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../../bases/apps/
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "clusters", "local", "apps", "kustomization.yaml"),
		[]byte(clusterAppsKustomization),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write apps kustomization: %v", err)
	}

	variablesKustomization := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - variables-cluster-config-map.yaml
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "clusters", "local", "variables", "kustomization.yaml"),
		[]byte(variablesKustomization),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write variables kustomization: %v", err)
	}

	variablesConfigMap := `apiVersion: v1
kind: ConfigMap
metadata:
  name: variables-cluster
  namespace: flux-system
data:
  auth_proxy_replicas: "3"
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "clusters", "local", "variables", "variables-cluster-config-map.yaml"),
		[]byte(variablesConfigMap),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write variables configmap: %v", err)
	}

	cmd := workload.NewValidateCmd()
	cmd.SetArgs([]string{tmpDir})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("expected validation to succeed with Flux substitutions, got error: %v\noutput: %s", err, output.String())
	}
}
