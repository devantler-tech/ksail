package workload_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/testutil/homeenv"
	snapshottest "github.com/devantler-tech/ksail/v7/internal/testutil/snapshottest"
	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/fsnotify/fsnotify"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	workloadCommandName  = "workload"
	pushCommandName      = "push"
	reconcileCommandName = "reconcile"
)

//nolint:gochecknoglobals // Serializes t.Chdir-based config discovery tests in this package.
var workloadConfigDiscoveryMu sync.Mutex

func TestNewImagesCmdHasCorrectDefaults(t *testing.T) {
	t.Parallel()

	cmd := workload.NewImagesCmd()

	if cmd.Use != "images" {
		t.Fatalf("expected Use to be %q, got %q", "images", cmd.Use)
	}

	if cmd.Short != "List container images required by cluster components" {
		t.Fatalf("expected Short description %q, got %q",
			"List container images required by cluster components", cmd.Short)
	}

	if !cmd.SilenceUsage {
		t.Fatal("expected SilenceUsage to be true")
	}

	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag to exist")
	}

	if outputFlag.DefValue != "plain" {
		t.Fatalf("expected --output flag default value to be %q, got %q",
			"plain", outputFlag.DefValue)
	}

	if outputFlag.Shorthand != "o" {
		t.Fatalf("expected --output shorthand to be %q, got %q", "o", outputFlag.Shorthand)
	}
}

func TestImagesCmdShowsHelp(t *testing.T) {
	t.Parallel()

	cmd := workload.NewImagesCmd()

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error executing images --help, got %v", err)
	}

	snaps.MatchSnapshot(t, normalizeHomePaths(output.String()))
}

func TestImagesCmdAcceptsValidOutputFormats(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "default is plain",
			args:     []string{},
			expected: "plain",
		},
		{
			name:     "explicit plain",
			args:     []string{"--output=plain"},
			expected: "plain",
		},
		{
			name:     "json format",
			args:     []string{"--output=json"},
			expected: "json",
		},
		{
			name:     "shorthand -o json",
			args:     []string{"-o", "json"},
			expected: "json",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := workload.NewImagesCmd()

			err := cmd.ParseFlags(testCase.args)
			if err != nil {
				t.Fatalf("expected no error parsing flags %v, got %v", testCase.args, err)
			}

			got, err := cmd.Flags().GetString("output")
			if err != nil {
				t.Fatalf("expected no error getting output flag, got %v", err)
			}

			if got != testCase.expected {
				t.Fatalf("expected output flag %q, got %q", testCase.expected, got)
			}
		})
	}
}

func TestErrUnknownOutputFormatIsSentinelError(t *testing.T) {
	t.Parallel()

	if workload.ErrUnknownOutputFormat == nil {
		t.Fatal("expected ErrUnknownOutputFormat to be a non-nil sentinel error")
	}

	if workload.ErrUnknownOutputFormat.Error() == "" {
		t.Fatal("expected ErrUnknownOutputFormat.Error() to return a non-empty string")
	}

	wrapped := fmt.Errorf("wrapping: %w", workload.ErrUnknownOutputFormat)
	if !errors.Is(wrapped, workload.ErrUnknownOutputFormat) {
		t.Fatal("expected errors.Is to identify ErrUnknownOutputFormat through wrapping")
	}
}

func TestNewInstallCmdRequiresMinimumArgs(t *testing.T) {
	t.Parallel()

	cmd := workload.NewInstallCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected argument validation error")
	}
}

func TestInstallCommandUsesDefaultNamespace(t *testing.T) {
	t.Parallel()

	err := runInstallCmd(t, "release", "./missing-chart")
	if err == nil {
		t.Fatalf("expected installation error due to missing chart")
	}

	if !strings.Contains(err.Error(), "install chart \"./missing-chart\"") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallCommandHonorsFlags(t *testing.T) {
	t.Parallel()

	err := runInstallCmd(
		t,
		"release",
		"./still-missing",
		"--namespace",
		"team",
		"--create-namespace",
		"--wait",
		"--atomic",
	)
	if err == nil {
		t.Fatalf("expected installation error due to missing chart")
	}

	if !strings.Contains(err.Error(), "install chart \"./still-missing\"") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func runInstallCmd(t *testing.T, args ...string) error {
	t.Helper()

	cmd := workload.NewInstallCmd()

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	t.Cleanup(cancel)
	cmd.SetContext(ctx)
	cmd.SetArgs(args)

	err := cmd.Execute()
	if err != nil {
		return fmt.Errorf("execute install command: %w", err)
	}

	return nil
}

// TestWriteWorkloadCommandsHaveWritePermission verifies that each
// state-mutating workload command listed in testCases carries the "write"
// permission annotation. The AI toolgen system uses this annotation to
// classify commands into read/write tool groups (workload_read vs
// workload_write), which enables user-confirmation prompts before any
// destructive or mutating operation exposed through these commands.
func TestWriteWorkloadCommandsHaveWritePermission(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "apply", cmd: workload.NewApplyCmd()},
		{name: "create", cmd: workload.NewCreateCmd()},
		{name: "debug", cmd: workload.NewDebugCmd()},
		{name: "delete", cmd: workload.NewDeleteCmd()},
		{name: "export", cmd: workload.NewExportCmd()},
		{name: "edit", cmd: workload.NewEditCmd()},
		{name: "exec", cmd: workload.NewExecCmd()},
		{name: "expose", cmd: workload.NewExposeCmd()},
		{name: "import", cmd: workload.NewImportCmd()},
		{name: "install", cmd: workload.NewInstallCmd()},
		{name: pushCommandName, cmd: workload.NewPushCmd()},
		{name: reconcileCommandName, cmd: workload.NewReconcileCmd()},
		{name: "rollout", cmd: workload.NewRolloutCmd()},
		{name: "scale", cmd: workload.NewScaleCmd()},
		{name: "watch", cmd: workload.NewWatchCmd()},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			perm, ok := testCase.cmd.Annotations[annotations.AnnotationPermission]
			if !ok {
				t.Fatalf(
					"command %q is missing %q annotation; "+
						"add Annotations: map[string]string{annotations.AnnotationPermission: \"write\"}",
					testCase.name,
					annotations.AnnotationPermission,
				)
			}

			if perm != "write" {
				t.Fatalf(
					"command %q has permission %q, expected \"write\"",
					testCase.name,
					perm,
				)
			}
		})
	}
}

// TestReadWorkloadCommandsDoNotHaveWritePermission verifies that read-only
// workload commands do NOT carry the "ai.toolgen.permission" annotation at all.
// These commands must not require user confirmation in the AI toolgen system.
func TestReadWorkloadCommandsDoNotHaveWritePermission(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "describe", cmd: workload.NewDescribeCmd()},
		{name: "explain", cmd: workload.NewExplainCmd()},
		{name: "forward", cmd: workload.NewForwardCmd()},
		{name: "get", cmd: workload.NewGetCmd()},
		{name: "images", cmd: workload.NewImagesCmd()},
		{name: "logs", cmd: workload.NewLogsCmd()},
		{name: "wait", cmd: workload.NewWaitCmd()},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if _, hasAnnotation := testCase.cmd.Annotations[annotations.AnnotationPermission]; hasAnnotation {
				t.Fatalf(
					"read-only command %q must not have the %q annotation set; "+
						"remove Annotations: map[string]string{annotations.AnnotationPermission: ...}",
					testCase.name,
					annotations.AnnotationPermission,
				)
			}
		})
	}
}

func TestNewPushCmdHasValidateFlag(t *testing.T) {
	t.Parallel()

	cmd := workload.NewPushCmd()

	// Check if --validate flag exists
	validateFlag := cmd.Flags().Lookup("validate")
	if validateFlag == nil {
		t.Fatal("expected --validate flag to exist")
	}

	// Check default value
	if validateFlag.DefValue != "false" {
		t.Fatalf(
			"expected --validate flag default value to be false, got %s",
			validateFlag.DefValue,
		)
	}

	// Check usage text
	expectedUsage := "Validate manifests before pushing"
	if validateFlag.Usage != expectedUsage {
		t.Fatalf(
			"expected --validate flag usage to be %q, got %q",
			expectedUsage,
			validateFlag.Usage,
		)
	}
}

func TestPushCmdShowsValidateFlagInHelp(t *testing.T) {
	t.Parallel()

	cmd := workload.NewPushCmd()

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error executing push --help, got %v", err)
	}

	snaps.MatchSnapshot(t, output.String())
}

func TestPushCmdAcceptsValidateFlag(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "validate flag not set",
			args:     []string{},
			expected: false,
		},
		{
			name:     "validate flag set to true",
			args:     []string{"--validate=true"},
			expected: true,
		},
		{
			name:     "validate flag set to false",
			args:     []string{"--validate=false"},
			expected: false,
		},
		{
			name:     "validate flag shorthand",
			args:     []string{"--validate"},
			expected: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := workload.NewPushCmd()
			cmd.SetArgs(testCase.args)

			// Parse flags without executing the command
			err := cmd.ParseFlags(testCase.args)
			if err != nil {
				t.Fatalf("expected no error parsing flags, got %v", err)
			}

			validate, err := cmd.Flags().GetBool("validate")
			if err != nil {
				t.Fatalf("expected no error getting validate flag, got %v", err)
			}

			if validate != testCase.expected {
				t.Fatalf("expected validate flag to be %v, got %v", testCase.expected, validate)
			}
		})
	}
}

func TestExpandFluxSubstitutionsNoVars(t *testing.T) {
	t.Parallel()

	input := []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n")
	result := workload.ExportExpandFluxSubstitutions(input)
	assert.Equal(t, input, result)
}

func TestExpandFluxSubstitutionsDefaultSyntax(t *testing.T) {
	t.Parallel()

	input := []byte(
		"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: test\nspec:\n  replicas: ${count:=3}\n",
	)
	result := workload.ExportExpandFluxSubstitutions(input)
	resultStr := string(result)
	// Do not rely on schema-driven typing (integer vs string); just ensure substitution happened
	assert.NotContains(t, resultStr, "${count")
	assert.Contains(t, resultStr, "replicas:")
	assert.Contains(t, resultStr, "3")
}

func TestExpandFluxSubstitutionsDefaultHyphenSyntax(t *testing.T) {
	t.Parallel()

	input := []byte(
		"apiVersion: v1\nkind: Service\nmetadata:\n  name: ${svc_name:-my-service}\n",
	)
	result := workload.ExportExpandFluxSubstitutions(input)
	assert.Contains(t, string(result), "name: my-service")
}

func TestExpandFluxSubstitutionsBareVarStringField(t *testing.T) {
	t.Parallel()

	input := []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: ${my_name}\n")
	result := workload.ExportExpandFluxSubstitutions(input)
	resultStr := string(result)
	assert.Contains(t, resultStr, "name: placeholder")
	assert.NotContains(t, resultStr, "${my_name}")
}

func TestExpandFluxSubstitutionsBareVarIntegerField(t *testing.T) {
	t.Parallel()

	input := []byte(
		"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: test\nspec:\n  replicas: ${count}\n",
	)
	result := workload.ExportExpandFluxSubstitutions(input)
	resultStr := string(result)
	// Should substitute with a value (0 if schema available, placeholder otherwise)
	assert.NotContains(t, resultStr, "${count}")
}

func TestExpandFluxSubstitutionsMixedText(t *testing.T) {
	t.Parallel()

	input := []byte(
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\ndata:\n  host: whoami.${domain}\n",
	)
	result := workload.ExportExpandFluxSubstitutions(input)
	resultStr := string(result)
	assert.Contains(t, resultStr, "whoami.placeholder")
	assert.NotContains(t, resultStr, "${domain}")
}

func TestExpandFluxSubstitutionsMultipleVarsInOneLine(t *testing.T) {
	t.Parallel()

	input := []byte(
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\ndata:\n  url: https://${sub}.${domain}/path\n",
	)
	result := workload.ExportExpandFluxSubstitutions(input)
	resultStr := string(result)
	assert.Contains(t, resultStr, "https://placeholder.placeholder/path")
}

func TestExpandFluxSubstitutionsFallbackOnBadYAML(t *testing.T) {
	t.Parallel()

	input := []byte("not: valid: yaml: ${var}\n[broken")
	result := workload.ExportExpandFluxSubstitutions(input)
	resultStr := string(result)
	assert.NotContains(t, resultStr, "${var}")
}

func TestExpandFluxSubstitutionsMultiDoc(t *testing.T) {
	t.Parallel()

	input := []byte("apiVersion: v1\nkind: ConfigMap\n" +
		"metadata:\n  name: ${name1}\n---\n" +
		"apiVersion: v1\nkind: ConfigMap\n" +
		"metadata:\n  name: ${name2}\n",
	)
	result := workload.ExportExpandFluxSubstitutions(input)
	resultStr := string(result)
	assert.NotContains(t, resultStr, "${name1}")
	assert.NotContains(t, resultStr, "${name2}")
}

func TestExpandFluxSubstitutionsEnvIgnoredDefaultHyphenSyntax(t *testing.T) {
	t.Setenv("svc_name", "real-service")

	input := []byte(
		"apiVersion: v1\nkind: Service\nmetadata:\n  name: ${svc_name:-my-service}\n",
	)
	result := workload.ExportExpandFluxSubstitutions(input)
	resultStr := string(result)
	assert.Contains(t, resultStr, "name: my-service")
	assert.NotContains(t, resultStr, "name: real-service")
}

func TestExpandFluxSubstitutionsEnvIgnoredDefaultEqualsSyntax(t *testing.T) {
	t.Setenv("svc_name", "real-service")

	input := []byte(
		"apiVersion: v1\nkind: Service\nmetadata:\n  name: ${svc_name:=my-service}\n",
	)
	result := workload.ExportExpandFluxSubstitutions(input)
	resultStr := string(result)
	assert.Contains(t, resultStr, "name: my-service")
	assert.NotContains(t, resultStr, "name: real-service")
}

func TestExportGetSchemaTypeAtPath(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"properties": map[string]any{
			"spec": map[string]any{
				"properties": map[string]any{
					"replicas": map[string]any{
						"type": "integer",
					},
					"paused": map[string]any{
						"type": "boolean",
					},
					"hostnames": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"integer field", "/spec/replicas", "integer"},
		{"boolean field", "/spec/paused", "boolean"},
		{"array item", "/spec/hostnames/0", "string"},
		{"unknown field", "/spec/unknown", ""},
		{"nonexistent path", "/nonexistent/path", ""},
		{"empty path", "", ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := workload.ExportGetSchemaTypeAtPath(schema, testCase.path)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestExportGetSchemaTypeAtPathNilSchema(t *testing.T) {
	t.Parallel()
	assert.Empty(t, workload.ExportGetSchemaTypeAtPath(nil, "/spec/replicas"))
}

func TestExportSchemaURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		apiVersion string
		kind       string
		contains   string
	}{
		{"core resource", "v1", "Service", "kubernetes-json-schema"},
		{
			"apps group", "apps/v1", "Deployment",
			"deployment-apps-v1.json",
		},
		{
			"CRD", "gateway.networking.k8s.io/v1", "HTTPRoute",
			"httproute-gateway.networking.k8s.io-v1.json",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			urls := workload.ExportSchemaURLs(testCase.apiVersion, testCase.kind)
			require.NotEmpty(t, urls)
			assert.Contains(t, urls[0], testCase.contains)
		})
	}
}

func TestExportSplitAPIVersion(t *testing.T) {
	t.Parallel()

	group, version := workload.ExportSplitAPIVersion("apps/v1")
	assert.Equal(t, "apps", group)
	assert.Equal(t, "v1", version)

	group, version = workload.ExportSplitAPIVersion("v1")
	assert.Empty(t, group)
	assert.Equal(t, "v1", version)

	group, version = workload.ExportSplitAPIVersion("gateway.networking.k8s.io/v1")
	assert.Equal(t, "gateway.networking.k8s.io", group)
	assert.Equal(t, "v1", version)
}

func TestExportTypedPlaceholderValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		schemaType string
		expected   any
	}{
		{"string", "placeholder"},
		{"integer", 0},
		{"number", 0.0},
		{"boolean", true},
		{"unknown", "placeholder"},
		{"", "placeholder"},
	}

	for _, testCase := range tests {
		t.Run(testCase.schemaType, func(t *testing.T) {
			t.Parallel()

			result := workload.ExportTypedPlaceholderValue(testCase.schemaType)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

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

// invalidConfigMapManifest has a string `data` where a map is required, so it
// fails kubeconform validation (a type violation, even in non-strict mode)
// unless the ConfigMap kind is skipped.
const invalidConfigMapManifest = `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data: "invalid structure"
`

//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() - they are incompatible
func TestValidateCmdWithSkipKindsFlag(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.WriteFile(
		filepath.Join(tmpDir, "configmap.yaml"),
		[]byte(invalidConfigMapManifest),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	// Chdir so config discovery starts from an empty (config-less) directory.
	t.Chdir(tmpDir)

	// Without skipping the kind, validation fails.
	failCmd := workload.NewValidateCmd()
	failCmd.SetArgs([]string{"."})

	var failOut bytes.Buffer

	failCmd.SetOut(&failOut)
	failCmd.SetErr(&failOut)

	err = failCmd.Execute()
	if err == nil {
		t.Fatal("expected validation to fail without --skip-kinds")
	}

	// With --skip-kinds=ConfigMap the resource is skipped and validation passes.
	passCmd := workload.NewValidateCmd()
	passCmd.SetArgs([]string{"--skip-kinds=ConfigMap", "."})

	var passOut bytes.Buffer

	passCmd.SetOut(&passOut)
	passCmd.SetErr(&passOut)

	err = passCmd.Execute()
	if err != nil {
		t.Fatalf("expected validation with --skip-kinds=ConfigMap to succeed, got: %v", err)
	}
}

//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() - they are incompatible
func TestValidateCmdWithSkipKindsFromConfig(t *testing.T) {
	tmpDir := t.TempDir()

	manifestDir := filepath.Join(tmpDir, "k8s")

	err := os.MkdirAll(manifestDir, 0o750)
	if err != nil {
		t.Fatalf("failed to create manifest dir: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(manifestDir, "configmap.yaml"),
		[]byte(invalidConfigMapManifest),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	// ksail.yaml in the working directory declares the kind to skip.
	ksailYAML := `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: test
spec:
  workload:
    validation:
      skipKinds:
        - ConfigMap
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "ksail.yaml"),
		[]byte(ksailYAML),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write ksail.yaml: %v", err)
	}

	t.Chdir(tmpDir)

	cmd := workload.NewValidateCmd()
	cmd.SetArgs([]string{"k8s"})

	var output bytes.Buffer

	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err = cmd.Execute()
	if err != nil {
		t.Fatalf(
			"expected validation with spec.workload.validation.skipKinds to succeed, got: %v",
			err,
		)
	}
}

//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() - they are incompatible
func TestValidateCmdWarnsOnUnreadableConfig(t *testing.T) {
	tmpDir := t.TempDir()

	manifestDir := filepath.Join(tmpDir, "k8s")

	err := os.MkdirAll(manifestDir, 0o750)
	if err != nil {
		t.Fatalf("failed to create manifest dir: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(manifestDir, "namespace.yaml"),
		[]byte(validNamespaceManifest),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	// A ksail.yaml that exists but does not parse.
	err = os.WriteFile(filepath.Join(tmpDir, "ksail.yaml"), []byte("foo: [bar"), 0o600)
	if err != nil {
		t.Fatalf("failed to write ksail.yaml: %v", err)
	}

	t.Chdir(tmpDir)

	cmd := workload.NewValidateCmd()
	cmd.SetArgs([]string{"k8s"})

	var output bytes.Buffer

	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("expected validation to succeed despite unreadable config, got: %v", err)
	}

	if !strings.Contains(output.String(), "skipKinds") {
		t.Errorf("expected a warning mentioning skipKinds, got: %q", output.String())
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

//nolint:funlen,cyclop // integration-style setup test with intentional multi-step assertions
func TestValidateCmdSubstitutesFluxPostBuildVariables(
	t *testing.T,
) {
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
		filepath.Join(
			tmpDir,
			"clusters",
			"local",
			"variables",
			"variables-cluster-config-map.yaml",
		),
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
		t.Fatalf(
			"expected validation to succeed with Flux substitutions, got error: %v\noutput: %s",
			err,
			output.String(),
		)
	}
}

func TestValidateCmdSubstitutesFluxPostBuildVariablesWithDefaults(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	setupDefaultSubstitutionTestDir(t, tmpDir)

	cmd := workload.NewValidateCmd()
	cmd.SetArgs([]string{tmpDir})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cmd.Execute()
	require.NoError(
		t,
		err,
		"expected validation to succeed with Secret substitutions, output: %s",
		output.String(),
	)
}

// setupDefaultSubstitutionTestDir creates a Flux-like project structure used to
// validate default and env-var style expansion in manifests. The fixture includes
// ConfigMap and Secret manifests that resemble substituteFrom sources for realism,
// but the current validate implementation does not read or use those resources.
//
//nolint:funlen // test setup helper builds full fixture tree for readability
func setupDefaultSubstitutionTestDir(
	t *testing.T,
	tmpDir string,
) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "bases", "apps"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "clusters", "local", "apps"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "clusters", "local", "variables"), 0o750))

	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, "bases", "apps", "kustomization.yaml"),
		[]byte(
			"apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - deployment.yaml\n",
		),
		0o600,
	))

	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, "bases", "apps", "deployment.yaml"),
		[]byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
spec:
  replicas: ${replicas:=1}
  selector:
    matchLabels:
      app: myapp
  template:
    metadata:
      labels:
        app: myapp
    spec:
      containers:
        - name: myapp
          image: nginx:1.27.1
          env:
            - name: DB_HOST
              value: ${db_host:=localhost}
            - name: API_KEY
              value: ${api_key:=default}
`),
		0o600,
	))

	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, "clusters", "local", "apps", "flux-kustomization.yaml"),
		[]byte("apiVersion: kustomize.toolkit.fluxcd.io/v1\nkind: Kustomization\nmetadata:\n"+
			"  name: apps\n  namespace: flux-system\nspec:\n  interval: 60m\n  prune: true\n"+
			"  postBuild:\n    substituteFrom:\n      - kind: ConfigMap\n        name: vars-cluster\n"+
			"      - kind: Secret\n        name: vars-secret\n  sourceRef:\n    kind: OCIRepository\n"+
			"    name: flux-system\n  path: clusters/local/apps/\n"),
		0o600,
	))

	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, "clusters", "local", "apps", "kustomization.yaml"),
		[]byte(
			"apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - ../../../bases/apps/\n",
		),
		0o600,
	))

	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, "clusters", "local", "variables", "kustomization.yaml"),
		[]byte("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n"+
			"resources:\n  - vars-cluster.yaml\n  - vars-secret.yaml\n"),
		0o600,
	))

	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, "clusters", "local", "variables", "vars-cluster.yaml"),
		[]byte(
			"apiVersion: v1\nkind: ConfigMap\nmetadata:\n"+
				"  name: vars-cluster\n  namespace: flux-system\n"+
				"data:\n  replicas: \"3\"\n  db_host: \"db.example.com\"\n",
		),
		0o600,
	))

	// Secret with base64-encoded .data (api_key = "s3cret" base64-encoded)
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, "clusters", "local", "variables", "vars-secret.yaml"),
		[]byte(
			"apiVersion: v1\nkind: Secret\nmetadata:\n"+
				"  name: vars-secret\n  namespace: flux-system\n"+
				"type: Opaque\ndata:\n  api_key: czNjcmV0\n",
		),
		0o600,
	))
}

func TestNewWatchCmdHasCorrectDefaults(t *testing.T) {
	t.Parallel()

	cmd := workload.NewWatchCmd()

	require.Equal(t, "watch", cmd.Use)
	require.Equal(
		t,
		"Watch for file changes and auto-apply workloads",
		cmd.Short,
	)

	pathFlag := cmd.Flags().Lookup("path")
	require.NotNil(t, pathFlag, "expected --path flag to exist")
	require.Empty(t, pathFlag.DefValue)

	initialApplyFlag := cmd.Flags().Lookup("initial-apply")
	require.NotNil(t, initialApplyFlag, "expected --initial-apply flag to exist")
	require.Equal(t, "false", initialApplyFlag.DefValue)
}

func TestWatchCmdShowsHelp(t *testing.T) {
	t.Parallel()

	cmd := workload.NewWatchCmd()

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	snaps.MatchSnapshot(t, output.String())
}

func TestWatchCmdRejectsArguments(t *testing.T) {
	t.Parallel()

	cmd := workload.NewWatchCmd()
	cmd.SetArgs([]string{"extra-arg"})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown command")
}

func TestIsRelevantEvent(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		event    fsnotify.Event
		expected bool
	}{
		{
			name:     "write event is relevant",
			event:    fsnotify.Event{Name: "f.yaml", Op: fsnotify.Write},
			expected: true,
		},
		{
			name:     "create event is relevant",
			event:    fsnotify.Event{Name: "f.yaml", Op: fsnotify.Create},
			expected: true,
		},
		{
			name:     "remove event is relevant",
			event:    fsnotify.Event{Name: "f.yaml", Op: fsnotify.Remove},
			expected: true,
		},
		{
			name:     "rename event is relevant",
			event:    fsnotify.Event{Name: "f.yaml", Op: fsnotify.Rename},
			expected: true,
		},
		{
			name:     "chmod event is not relevant",
			event:    fsnotify.Event{Name: "f.yaml", Op: fsnotify.Chmod},
			expected: false,
		},
		{
			name:     "no op is not relevant",
			event:    fsnotify.Event{Name: "f.yaml", Op: 0},
			expected: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportIsRelevantEvent(testCase.event)
			require.Equal(t, testCase.expected, got)
		})
	}
}

func TestFormatElapsed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "sub-second duration",
			duration: 300 * time.Millisecond,
			expected: "0.3s",
		},
		{
			name:     "just over one second",
			duration: 1200 * time.Millisecond,
			expected: "1.2s",
		},
		{
			name:     "whole seconds",
			duration: 5 * time.Second,
			expected: "5.0s",
		},
		{
			name:     "longer apply",
			duration: 45500 * time.Millisecond,
			expected: "45.5s",
		},
		{
			name:     "zero duration",
			duration: 0,
			expected: "0.0s",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportFormatElapsed(testCase.duration)
			require.Equal(t, testCase.expected, got)
		})
	}
}

func TestResolveSourceDir(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		pathFlag string
		srcDir   string
		expected string
	}{
		{
			name:     "flag takes precedence",
			pathFlag: "./custom",
			srcDir:   "configured",
			expected: "./custom",
		},
		{
			name:     "config fallback",
			pathFlag: "",
			srcDir:   "from-config",
			expected: "from-config",
		},
		{
			name:     "default when both empty",
			pathFlag: "",
			srcDir:   "",
			expected: v1alpha1.DefaultSourceDirectory,
		},
		{
			name:     "whitespace-only flag uses config",
			pathFlag: "   ",
			srcDir:   "from-config",
			expected: "from-config",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := &v1alpha1.Cluster{}
			cfg.Spec.Workload.SourceDirectory = testCase.srcDir

			got := workload.ExportResolveSourceDir(cfg, testCase.pathFlag)
			require.Equal(t, testCase.expected, got)
		})
	}
}

func TestAddRecursiveWatchesSubdirectories(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	subDir := filepath.Join(tmpDir, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0o750))

	nestedDir := filepath.Join(subDir, "nested")
	require.NoError(t, os.MkdirAll(nestedDir, 0o750))

	// Create a file to ensure files are skipped (only dirs watched).
	filePath := filepath.Join(tmpDir, "test.yaml")
	require.NoError(t, os.WriteFile(filePath, []byte("test"), 0o600))

	watcher, err := fsnotify.NewWatcher()
	require.NoError(t, err)

	defer func() { _ = watcher.Close() }()

	err = workload.ExportAddRecursive(watcher, tmpDir)
	require.NoError(t, err)

	// Verify the watcher has the expected directories.
	watchList := watcher.WatchList()
	require.Contains(t, watchList, tmpDir)
	require.Contains(t, watchList, subDir)
	require.Contains(t, watchList, nestedDir)
}

func TestAddRecursiveFailsOnMissingDir(t *testing.T) {
	t.Parallel()

	watcher, err := fsnotify.NewWatcher()
	require.NoError(t, err)

	defer func() { _ = watcher.Close() }()

	err = workload.ExportAddRecursive(watcher, "/nonexistent/path")
	require.Error(t, err)
}

func TestCancelPendingDebounce(t *testing.T) {
	t.Parallel()

	t.Run("increments_generation", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		workload.ExportCancelPendingDebounce(state)

		require.Equal(t, uint64(1), workload.ExportGetGeneration(state))
	})

	t.Run("each_call_increments_generation", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		workload.ExportCancelPendingDebounce(state)
		workload.ExportCancelPendingDebounce(state)
		workload.ExportCancelPendingDebounce(state)

		require.Equal(t, uint64(3), workload.ExportGetGeneration(state))
	})

	t.Run("nil_timer_does_not_panic", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()

		require.NotPanics(t, func() {
			workload.ExportCancelPendingDebounce(state)
		})
	})
}

func TestScheduleApply(t *testing.T) {
	t.Parallel()

	t.Run("updates_last_file", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		applyCh := make(chan string, 1)

		workload.ExportScheduleApply(state, "test.yaml", applyCh)
		workload.ExportCancelPendingDebounce(state)

		require.Equal(t, "test.yaml", workload.ExportGetLastFile(state))
	})

	t.Run("increments_generation", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		applyCh := make(chan string, 1)

		workload.ExportScheduleApply(state, "test.yaml", applyCh)
		workload.ExportCancelPendingDebounce(state)

		// scheduleApply increments gen (→1), cancelPendingDebounce increments gen (→2).
		require.Equal(t, uint64(2), workload.ExportGetGeneration(state))
	})

	t.Run("replaces_previous_file", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		applyCh := make(chan string, 1)

		workload.ExportScheduleApply(state, "first.yaml", applyCh)
		workload.ExportScheduleApply(state, "second.yaml", applyCh)
		workload.ExportCancelPendingDebounce(state)

		require.Equal(t, "second.yaml", workload.ExportGetLastFile(state))
	})

	t.Run("enqueues_file_after_debounce_interval", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		applyCh := make(chan string, 1)

		workload.ExportScheduleApply(state, "apply.yaml", applyCh)

		select {
		case got := <-applyCh:
			require.Equal(t, "apply.yaml", got)
		case <-time.After(workload.ExportDebounceInterval + 500*time.Millisecond):
			t.Fatal("expected apply.yaml in channel within debounce interval")
		}
	})
}

func TestEnqueueIfCurrent(t *testing.T) {
	t.Parallel()

	t.Run("skips_stale_generation", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		workload.ExportSetDebounceState(state, 5, "test.yaml")

		applyCh := make(chan string, 1)

		workload.ExportEnqueueIfCurrent(state, 4, applyCh)

		select {
		case got := <-applyCh:
			t.Fatalf("expected empty channel for stale generation, got %q", got)
		default:
			// expected: stale generation was discarded
		}
	})

	t.Run("enqueues_for_matching_generation", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		workload.ExportSetDebounceState(state, 5, "test.yaml")

		applyCh := make(chan string, 1)

		workload.ExportEnqueueIfCurrent(state, 5, applyCh)

		select {
		case got := <-applyCh:
			require.Equal(t, "test.yaml", got)
		default:
			t.Fatal("expected test.yaml in channel for matching generation")
		}
	})

	t.Run("coalesces_stale_pending_apply", func(t *testing.T) {
		t.Parallel()

		state := workload.ExportNewDebounceState()
		workload.ExportSetDebounceState(state, 5, "latest.yaml")

		applyCh := make(chan string, 1)

		// Pre-fill channel with a stale entry.
		applyCh <- "stale.yaml"

		workload.ExportEnqueueIfCurrent(state, 5, applyCh)

		select {
		case got := <-applyCh:
			require.Equal(t, "latest.yaml", got, "stale entry should be replaced with latest file")
		default:
			t.Fatal("expected latest.yaml in channel")
		}
	})
}

func TestTryAddDirectory(t *testing.T) {
	t.Parallel()

	t.Run("skips_non_existent_path", func(t *testing.T) {
		t.Parallel()

		watcher, err := fsnotify.NewWatcher()
		require.NoError(t, err)

		defer func() { _ = watcher.Close() }()

		cmd := &cobra.Command{}

		var buf bytes.Buffer
		cmd.SetErr(&buf)

		require.NotPanics(t, func() {
			workload.ExportTryAddDirectory(watcher, "/nonexistent/path/xyz", cmd)
		})

		require.Empty(t, watcher.WatchList())
	})

	t.Run("skips_regular_file", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "test.yaml")
		require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o600))

		watcher, err := fsnotify.NewWatcher()
		require.NoError(t, err)

		defer func() { _ = watcher.Close() }()

		cmd := &cobra.Command{}
		workload.ExportTryAddDirectory(watcher, filePath, cmd)

		require.Empty(t, watcher.WatchList())
	})

	t.Run("adds_directory_to_watcher", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()

		watcher, err := fsnotify.NewWatcher()
		require.NoError(t, err)

		defer func() { _ = watcher.Close() }()

		cmd := &cobra.Command{}
		workload.ExportTryAddDirectory(watcher, tmpDir, cmd)

		require.Contains(t, watcher.WatchList(), tmpDir)
	})
}

func TestFindKustomizationDirReturnsSubtree(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	subDir := filepath.Join(root, "apps", "frontend")
	require.NoError(t, os.MkdirAll(subDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(subDir, "kustomization.yaml"), []byte("resources: []"), 0o600,
	))

	changedFile := filepath.Join(subDir, "deployment.yaml")
	require.NoError(t, os.WriteFile(changedFile, []byte("kind: Deployment"), 0o600))

	got := workload.ExportFindKustomizationDir(changedFile, root)
	require.Equal(t, subDir, got)
}

func TestFindKustomizationDirReturnsRootWhenKustomizationAtRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "kustomization.yaml"), []byte("resources: []"), 0o600,
	))

	changedFile := filepath.Join(root, "deployment.yaml")
	require.NoError(t, os.WriteFile(changedFile, []byte("kind: Deployment"), 0o600))

	got := workload.ExportFindKustomizationDir(changedFile, root)
	require.Equal(t, root, got)
}

func TestFindKustomizationDirReturnsRootWhenNoKustomizationFound(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	subDir := filepath.Join(root, "misc")
	require.NoError(t, os.MkdirAll(subDir, 0o750))

	changedFile := filepath.Join(subDir, "notes.yaml")
	require.NoError(t, os.WriteFile(changedFile, []byte("note: true"), 0o600))

	got := workload.ExportFindKustomizationDir(changedFile, root)
	require.Equal(t, root, got)
}

func TestFindKustomizationDirWalksUpToNearest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	// Create nested structure: root/apps/kustomization.yaml and root/apps/frontend/deep/file.yaml
	appsDir := filepath.Join(root, "apps")
	require.NoError(t, os.MkdirAll(appsDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(appsDir, "kustomization.yaml"), []byte("resources: []"), 0o600,
	))

	deepDir := filepath.Join(appsDir, "frontend", "deep")
	require.NoError(t, os.MkdirAll(deepDir, 0o750))

	changedFile := filepath.Join(deepDir, "service.yaml")
	require.NoError(t, os.WriteFile(changedFile, []byte("kind: Service"), 0o600))

	got := workload.ExportFindKustomizationDir(changedFile, root)
	require.Equal(t, appsDir, got)
}

func TestFindKustomizationDirPrefersNearestOverParent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	// Root has kustomization.yaml
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "kustomization.yaml"), []byte("resources: []"), 0o600,
	))

	// apps/frontend also has kustomization.yaml (closer to the changed file)
	frontendDir := filepath.Join(root, "apps", "frontend")
	require.NoError(t, os.MkdirAll(frontendDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(frontendDir, "kustomization.yaml"), []byte("resources: []"), 0o600,
	))

	changedFile := filepath.Join(frontendDir, "deployment.yaml")
	require.NoError(t, os.WriteFile(changedFile, []byte("kind: Deployment"), 0o600))

	got := workload.ExportFindKustomizationDir(changedFile, root)
	require.Equal(t, frontendDir, got)
}

func TestFindKustomizationDirSelfEditReturnsOwnDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	subDir := filepath.Join(root, "infra")
	require.NoError(t, os.MkdirAll(subDir, 0o750))

	kustomizationFile := filepath.Join(subDir, "kustomization.yaml")
	require.NoError(t, os.WriteFile(kustomizationFile, []byte("resources: []"), 0o600))

	got := workload.ExportFindKustomizationDir(kustomizationFile, root)
	require.Equal(t, subDir, got)
}

func TestFindKustomizationDirDirectoryEventStartsAtDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	subDir := filepath.Join(root, "apps")
	require.NoError(t, os.MkdirAll(subDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(subDir, "kustomization.yaml"), []byte("resources: []"), 0o600,
	))

	// Pass the directory path itself (as fsnotify does for some create/rename events).
	got := workload.ExportFindKustomizationDir(subDir, root)
	require.Equal(t, subDir, got)
}

func TestFindKustomizationDirDeletedFileFallsBack(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	subDir := filepath.Join(root, "apps")
	require.NoError(t, os.MkdirAll(subDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(subDir, "kustomization.yaml"), []byte("resources: []"), 0o600,
	))

	// Simulate a deleted file event — the file no longer exists on disk.
	deletedFile := filepath.Join(subDir, "removed.yaml")

	got := workload.ExportFindKustomizationDir(deletedFile, root)
	require.Equal(t, subDir, got)
}

func TestNormalizeFluxPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain relative path",
			input:    "apps/frontend",
			expected: "apps/frontend",
		},
		{
			name:     "dotslash prefix",
			input:    "./apps/frontend",
			expected: "apps/frontend",
		},
		{
			name:     "root dot",
			input:    ".",
			expected: "",
		},
		{
			name:     "dotslash only",
			input:    "./",
			expected: "",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "trailing slash cleaned",
			input:    "apps/frontend/",
			expected: "apps/frontend",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportNormalizeFluxPath(testCase.input)
			require.Equal(t, testCase.expected, got)
		})
	}
}

func TestMatchFluxKustomizationsExactMatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	changedDir := filepath.Join(root, "apps", "frontend")

	kustomizations := []flux.KustomizationInfo{
		{Name: "frontend", Path: "./apps/frontend"},
		{Name: "backend", Path: "./apps/backend"},
	}

	got := workload.ExportMatchFluxKustomizations(changedDir, root, kustomizations)
	require.Equal(t, []string{"frontend"}, got)
}

func TestMatchFluxKustomizationsParentChange(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	changedDir := filepath.Join(root, "apps")

	kustomizations := []flux.KustomizationInfo{
		{Name: "frontend", Path: "apps/frontend"},
		{Name: "backend", Path: "apps/backend"},
		{Name: "infra", Path: "infra/networking"},
	}

	got := workload.ExportMatchFluxKustomizations(changedDir, root, kustomizations)
	require.ElementsMatch(t, []string{"frontend", "backend"}, got)
}

func TestMatchFluxKustomizationsChildChange(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	changedDir := filepath.Join(root, "apps", "frontend", "overlays")

	kustomizations := []flux.KustomizationInfo{
		{Name: "frontend", Path: "apps/frontend"},
	}

	got := workload.ExportMatchFluxKustomizations(changedDir, root, kustomizations)
	require.Equal(t, []string{"frontend"}, got)
}

func TestMatchFluxKustomizationsNoMatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	changedDir := filepath.Join(root, "unrelated")

	kustomizations := []flux.KustomizationInfo{
		{Name: "frontend", Path: "apps/frontend"},
		{Name: "backend", Path: "apps/backend"},
	}

	got := workload.ExportMatchFluxKustomizations(changedDir, root, kustomizations)
	require.Empty(t, got)
}

func TestMatchFluxKustomizationsRootChange(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	kustomizations := []flux.KustomizationInfo{
		{Name: "frontend", Path: "apps/frontend"},
	}

	got := workload.ExportMatchFluxKustomizations(root, root, kustomizations)
	require.Empty(t, got, "root-level changes should return nil to trigger full reconcile fallback")
}

func TestMatchFluxKustomizationsMultipleMatches(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	changedDir := filepath.Join(root, "apps")

	kustomizations := []flux.KustomizationInfo{
		{Name: "frontend-prod", Path: "apps/frontend"},
		{Name: "frontend-dev", Path: "apps/frontend-dev"},
		{Name: "backend", Path: "apps/backend"},
		{Name: "monitoring", Path: "infra/monitoring"},
	}

	got := workload.ExportMatchFluxKustomizations(changedDir, root, kustomizations)
	require.ElementsMatch(t, []string{"frontend-prod", "frontend-dev", "backend"}, got)
}

func TestMatchFluxKustomizationsSkipsRootPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	changedDir := filepath.Join(root, "apps")

	kustomizations := []flux.KustomizationInfo{
		{Name: "root-ks", Path: "."},
		{Name: "frontend", Path: "apps/frontend"},
	}

	got := workload.ExportMatchFluxKustomizations(changedDir, root, kustomizations)
	require.Equal(t, []string{"frontend"}, got,
		"CRs with root-level paths (\".\") should be skipped by selective matching")
}

func TestMatchFluxKustomizationsNormalizesLeadingDotSlash(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	changedDir := filepath.Join(root, "apps", "frontend")

	kustomizations := []flux.KustomizationInfo{
		{Name: "with-dotslash", Path: "./apps/frontend"},
		{Name: "without-dotslash", Path: "apps/frontend"},
	}

	got := workload.ExportMatchFluxKustomizations(changedDir, root, kustomizations)
	require.ElementsMatch(t, []string{"with-dotslash", "without-dotslash"}, got)
}

//nolint:funlen // six focused subtests; splitting further would reduce readability
func TestHasKustomizationFile(t *testing.T) {
	t.Parallel()

	t.Run("returns_true_when_kustomization_yaml_exists", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "kustomization.yaml"), []byte("resources: []"), 0o600,
		))

		require.True(t, workload.ExportHasKustomizationFile(dir))
	})

	t.Run("returns_true_when_kustomization_yml_exists", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "kustomization.yml"), []byte("resources: []"), 0o600,
		))

		require.True(t, workload.ExportHasKustomizationFile(dir))
	})

	t.Run("returns_true_when_Kustomization_exists", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "Kustomization"), []byte("resources: []"), 0o600,
		))

		require.True(t, workload.ExportHasKustomizationFile(dir))
	})

	t.Run("returns_false_when_no_kustomization", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "deployment.yaml"), []byte("kind: Deployment"), 0o600,
		))

		require.False(t, workload.ExportHasKustomizationFile(dir))
	})

	t.Run("returns_false_for_nonexistent_dir", func(t *testing.T) {
		t.Parallel()

		require.False(
			t,
			workload.ExportHasKustomizationFile(filepath.Join(t.TempDir(), "nonexistent")),
		)
	})

	t.Run("returns_false_when_kustomization_yaml_is_a_directory", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "kustomization.yaml"), 0o700))

		require.False(t, workload.ExportHasKustomizationFile(dir))
	})
}

func TestBuildFileSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("captures_file_modification_times", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		fileA := filepath.Join(dir, "a.yaml")
		fileB := filepath.Join(dir, "b.yaml")

		require.NoError(t, os.WriteFile(fileA, []byte("a"), 0o600))
		require.NoError(t, os.WriteFile(fileB, []byte("b"), 0o600))

		snap := workload.ExportBuildFileSnapshot(dir)

		require.Len(t, snap, 2)
		require.Contains(t, snap, fileA)
		require.Contains(t, snap, fileB)
	})

	t.Run("skips_directories", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		subDir := filepath.Join(dir, "sub")
		require.NoError(t, os.Mkdir(subDir, 0o750))

		snap := workload.ExportBuildFileSnapshot(dir)

		require.Empty(t, snap)
	})

	t.Run("includes_nested_files", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		subDir := filepath.Join(dir, "sub")
		require.NoError(t, os.Mkdir(subDir, 0o750))

		nested := filepath.Join(subDir, "deploy.yaml")
		require.NoError(t, os.WriteFile(nested, []byte("kind: Deployment"), 0o600))

		snap := workload.ExportBuildFileSnapshot(dir)

		require.Len(t, snap, 1)
		require.Contains(t, snap, nested)
	})

	t.Run("empty_directory_returns_empty_snapshot", func(t *testing.T) {
		t.Parallel()

		snap := workload.ExportBuildFileSnapshot(t.TempDir())

		require.Empty(t, snap)
	})
}

func TestDetectChangedFileReturnsEmptyWhenNoChanges(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "a.yaml")
	require.NoError(t, os.WriteFile(filePath, []byte("a"), 0o600))

	snap := workload.ExportBuildFileSnapshot(dir)
	changed := workload.ExportDetectChangedFile(dir, snap)

	require.Empty(t, changed)
}

func TestDetectChangedFileDetectsModifiedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "a.yaml")
	require.NoError(t, os.WriteFile(filePath, []byte("a"), 0o600))

	snap := workload.ExportBuildFileSnapshot(dir)

	// Force a distinct mod time explicitly (some filesystems have 1s granularity).
	now := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(filePath, now, now))

	changed := workload.ExportDetectChangedFile(dir, snap)

	require.Equal(t, filePath, changed)
}

func TestDetectChangedFileDetectsNewFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	existingFile := filepath.Join(dir, "a.yaml")
	require.NoError(t, os.WriteFile(existingFile, []byte("a"), 0o600))

	snap := workload.ExportBuildFileSnapshot(dir)

	newFile := filepath.Join(dir, "b.yaml")
	require.NoError(t, os.WriteFile(newFile, []byte("b"), 0o600))

	changed := workload.ExportDetectChangedFile(dir, snap)

	require.Equal(t, newFile, changed)
}

func TestDetectChangedFileDetectsDeletedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "a.yaml")
	require.NoError(t, os.WriteFile(filePath, []byte("a"), 0o600))

	snap := workload.ExportBuildFileSnapshot(dir)

	require.NoError(t, os.Remove(filePath))

	changed := workload.ExportDetectChangedFile(dir, snap)

	require.Equal(t, filePath, changed)
	require.NotContains(t, snap, filePath)
}

func TestDetectChangedFileUpdatesSnapshotInPlace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "a.yaml")
	require.NoError(t, os.WriteFile(filePath, []byte("a"), 0o600))

	snap := workload.ExportBuildFileSnapshot(dir)

	// Modify the file.
	now := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(filePath, now, now))

	_ = workload.ExportDetectChangedFile(dir, snap)

	// Second scan should find no changes since snapshot was updated.
	changed := workload.ExportDetectChangedFile(dir, snap)

	require.Empty(t, changed)
}

func TestNewScanCmdHasCorrectDefaults(t *testing.T) {
	t.Parallel()

	cmd := workload.NewScanCmd()

	require.Equal(t, "scan [PATH]", cmd.Use)
	require.Equal(t, "Run security scans on Kubernetes manifests", cmd.Short)
	require.True(t, cmd.SilenceUsage)

	frameworkFlag := cmd.Flags().Lookup("framework")
	require.NotNil(t, frameworkFlag, "expected --framework flag to exist")
	require.Equal(t, "[nsa]", frameworkFlag.DefValue)

	formatFlag := cmd.Flags().Lookup("format")
	require.NotNil(t, formatFlag, "expected --format flag to exist")
	require.Equal(t, "pretty-printer", formatFlag.DefValue)

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag, "expected --output flag to exist")
	require.Empty(t, outputFlag.DefValue)
	require.Equal(t, "o", outputFlag.Shorthand)

	thresholdFlag := cmd.Flags().Lookup("compliance-threshold")
	require.NotNil(t, thresholdFlag, "expected --compliance-threshold flag to exist")
	require.Equal(t, "0", thresholdFlag.DefValue)

	verboseFlag := cmd.Flags().Lookup("verbose")
	require.NotNil(t, verboseFlag, "expected --verbose flag to exist")
	require.Equal(t, "false", verboseFlag.DefValue)

	exceptionsFlag := cmd.Flags().Lookup("exceptions")
	require.NotNil(t, exceptionsFlag, "expected --exceptions flag to exist")
	require.Empty(t, exceptionsFlag.DefValue)
}

func TestScanCmdShowsHelp(t *testing.T) {
	t.Parallel()

	cmd := workload.NewScanCmd()

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error executing scan --help, got %v", err)
	}

	snaps.MatchSnapshot(t, normalizeHomePaths(output.String()))
}

func TestScanCmdRejectsMultiplePaths(t *testing.T) {
	t.Parallel()

	cmd := workload.NewScanCmd()

	cmd.SetArgs([]string{
		"/some/path1",
		"/some/path2",
	})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when multiple paths are provided")
	}
}

func TestScanCmdRejectsInvalidThreshold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		threshold string
	}{
		{name: "negative", threshold: "-1"},
		{name: "above 100", threshold: "101"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := workload.NewScanCmd()

			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"--compliance-threshold", testCase.threshold, "/nonexistent"})

			err := cmd.Execute()
			require.Error(t, err, "expected error for threshold %s", testCase.threshold)
			require.Contains(t, err.Error(), "--compliance-threshold must be between 0 and 100")
		})
	}
}

// scanConfigCluster builds a Cluster carrying the given spec.workload.scan
// settings for the scan settings-merge tests.
func scanConfigCluster(frameworks []string, threshold *int32, exceptions string) *v1alpha1.Cluster {
	return &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Workload: v1alpha1.WorkloadSpec{
				Scan: v1alpha1.ScanConfig{
					Frameworks:          frameworks,
					ComplianceThreshold: threshold,
					Exceptions:          exceptions,
				},
			},
		},
	}
}

// TestScanSettingsConfigUsedWhenNoFlags asserts spec.workload.scan supplies the
// frameworks and compliance threshold for a turnkey 'ksail workload scan' (no
// flags), so the config alone can gate at 100.
func TestScanSettingsConfigUsedWhenNoFlags(t *testing.T) {
	t.Parallel()

	threshold := int32(100)
	cfg := scanConfigCluster([]string{"mitre"}, &threshold, "")

	scanCmd := workload.NewScanCmd()
	require.NoError(t, scanCmd.ParseFlags(nil))

	got, err := workload.ExportResolveScanSettings(scanCmd, cfg, true)
	require.NoError(t, err)
	require.Equal(t, []string{"mitre"}, got.Frameworks)
	require.InDelta(t, float32(100), got.ComplianceThreshold, 0)
	require.Empty(t, got.Exceptions)
}

// TestScanSettingsFlagsOverrideConfig asserts an explicit flag wins over the
// configured value (flag > config precedence).
func TestScanSettingsFlagsOverrideConfig(t *testing.T) {
	t.Parallel()

	threshold := int32(100)
	cfg := scanConfigCluster([]string{"mitre"}, &threshold, "")

	scanCmd := workload.NewScanCmd()
	require.NoError(t, scanCmd.ParseFlags(
		[]string{"--framework", "cis", "--compliance-threshold", "50"},
	))

	got, err := workload.ExportResolveScanSettings(scanCmd, cfg, true)
	require.NoError(t, err)
	require.Equal(t, []string{"cis"}, got.Frameworks)
	require.InDelta(t, float32(50), got.ComplianceThreshold, 0)
}

// TestScanSettingsDefaultsWhenNoConfig asserts the flag defaults apply when no
// config file is found (frameworks default to nsa, threshold to 0).
func TestScanSettingsDefaultsWhenNoConfig(t *testing.T) {
	t.Parallel()

	scanCmd := workload.NewScanCmd()
	require.NoError(t, scanCmd.ParseFlags(nil))

	got, err := workload.ExportResolveScanSettings(scanCmd, nil, false)
	require.NoError(t, err)
	require.Equal(t, []string{"nsa"}, got.Frameworks)
	require.InDelta(t, float32(0), got.ComplianceThreshold, 0)
	require.Empty(t, got.Exceptions)
}

// TestScanSettingsRejectsInvalidConfigThreshold asserts an out-of-range
// complianceThreshold from config is rejected (not just the flag).
func TestScanSettingsRejectsInvalidConfigThreshold(t *testing.T) {
	t.Parallel()

	threshold := int32(150)
	cfg := scanConfigCluster(nil, &threshold, "")

	scanCmd := workload.NewScanCmd()
	require.NoError(t, scanCmd.ParseFlags(nil))

	_, err := workload.ExportResolveScanSettings(scanCmd, cfg, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--compliance-threshold must be between 0 and 100")
}

// TestScanSettingsExceptionsFromConfigCanonicalized asserts a configured
// exceptions path is canonicalized (symlink-resolved) before being forwarded.
func TestScanSettingsExceptionsFromConfigCanonicalized(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	exceptionsFile := filepath.Join(tempDir, "exceptions.json")
	require.NoError(t, os.WriteFile(exceptionsFile, []byte("[]"), 0o600))

	cfg := scanConfigCluster(nil, nil, exceptionsFile)

	scanCmd := workload.NewScanCmd()
	require.NoError(t, scanCmd.ParseFlags(nil))

	got, err := workload.ExportResolveScanSettings(scanCmd, cfg, true)
	require.NoError(t, err)

	canon, err := filepath.EvalSymlinks(exceptionsFile)
	require.NoError(t, err)
	require.Equal(t, canon, got.Exceptions)
}

// TestScanSettingsExceptionsFlagOverridesConfig asserts the --exceptions flag
// wins over a configured exceptions path.
func TestScanSettingsExceptionsFlagOverridesConfig(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	flagFile := filepath.Join(tempDir, "flag-exceptions.json")
	require.NoError(t, os.WriteFile(flagFile, []byte("[]"), 0o600))

	configFile := filepath.Join(tempDir, "config-exceptions.json")
	require.NoError(t, os.WriteFile(configFile, []byte("[]"), 0o600))

	cfg := scanConfigCluster(nil, nil, configFile)

	scanCmd := workload.NewScanCmd()
	require.NoError(t, scanCmd.ParseFlags([]string{"--exceptions", flagFile}))

	got, err := workload.ExportResolveScanSettings(scanCmd, cfg, true)
	require.NoError(t, err)

	canon, err := filepath.EvalSymlinks(flagFile)
	require.NoError(t, err)
	require.Equal(t, canon, got.Exceptions)
}

func TestPollInterval(t *testing.T) {
	t.Parallel()

	require.Equal(t, 3*time.Second, workload.ExportPollInterval)
}

// normalizeHomePaths replaces the user's home directory in help output
// with a stable placeholder so snapshots are portable across machines and CI.
func normalizeHomePaths(content string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return content
	}

	return strings.ReplaceAll(content, home, "$HOME")
}

func TestMain(m *testing.M) {
	os.Exit(homeenv.RunFunc(func() int {
		return snapshottest.Run(m, snaps.CleanOpts{Sort: true})
	}))
}

func writeValidKsailConfig(t *testing.T, dir string) {
	t.Helper()

	workloadDir := filepath.Join(dir, "k8s")
	require.NoError(t, os.MkdirAll(workloadDir, 0o750))

	ksailConfigContent := "apiVersion: ksail.io/v1alpha1\n" +
		"kind: Cluster\n" +
		"spec:\n" +
		"  distribution: Vanilla\n" +
		"  distributionConfig: kind.yaml\n" +
		"  sourceDirectory: k8s\n"

	configPath := filepath.Join(dir, "ksail.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(ksailConfigContent), 0o600))

	kindConfigContent := "kind: Cluster\n" +
		"apiVersion: kind.x-k8s.io/v1alpha4\n" +
		"name: kind\n"

	kindConfigPath := filepath.Join(dir, "kind.yaml")
	require.NoError(t, os.WriteFile(kindConfigPath, []byte(kindConfigContent), 0o600))
}

func writeFluxReconcileKsailConfig(t *testing.T, dir string) {
	t.Helper()

	writeValidKsailConfig(t, dir)

	ksailConfigContent := fmt.Sprintf(
		"apiVersion: ksail.io/v1alpha1\n"+
			"kind: Cluster\n"+
			"spec:\n"+
			"  cluster:\n"+
			"    distribution: Vanilla\n"+
			"    distributionConfig: kind.yaml\n"+
			"    gitOpsEngine: Flux\n"+
			"    connection:\n"+
			"      kubeconfig: %s\n"+
			"  workload:\n"+
			"    sourceDirectory: k8s\n",
		filepath.Join(dir, "missing-kubeconfig"),
	)

	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(dir, "ksail.yaml"),
			[]byte(ksailConfigContent),
			0o600,
		),
	)
}

func TestWorkloadHelpSnapshots(t *testing.T) {
	t.Parallel()

	// Walk the registered Cobra command tree instead of hand-maintaining a case list, so the
	// help snapshots automatically cover every (nested) workload subcommand as commands are
	// added, removed, or renamed.
	enumRoot := cmd.NewRootCmd("test", "test", "test")

	workloadCmd, _, err := enumRoot.Find([]string{workloadCommandName})
	require.NoError(t, err, "expected to find the workload command")

	for _, args := range collectHelpSnapshotArgs(workloadCmd, []string{workloadCommandName}) {
		name := strings.Join(args[1:], "_")
		if name == "" {
			name = workloadCommandName
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			root := cmd.NewRootCmd("test", "test", "test")
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs(append(append([]string{}, args...), "--help"))

			err := root.Execute()
			require.NoErrorf(
				t,
				err,
				"expected no error executing %s help",
				strings.Join(args, " "),
			)

			snaps.MatchSnapshot(t, normalizeHomePaths(out.String()))
		})
	}
}

// collectHelpSnapshotArgs walks the available (visible, non-deprecated) command tree rooted at
// current and returns the CLI args reaching each command, depth-first. path holds the args that
// reach current from the root command.
func collectHelpSnapshotArgs(current *cobra.Command, path []string) [][]string {
	argSets := [][]string{path}

	for _, sub := range current.Commands() {
		if !sub.IsAvailableCommand() {
			continue
		}

		subPath := make([]string, 0, len(path)+1)
		subPath = append(subPath, path...)
		subPath = append(subPath, sub.Name())

		argSets = append(argSets, collectHelpSnapshotArgs(sub, subPath)...)
	}

	return argSets
}

//nolint:paralleltest // Uses t.Chdir which is incompatible with parallel tests.
func TestWorkloadCommandsLoadConfigOnly(t *testing.T) {
	// Note: "apply" and "install" are excluded as they are full implementations with kubectl/helm wrappers
	testCases := []struct {
		name          string
		args          []string
		expectedError string
		writeConfig   func(t *testing.T, dir string)
	}{
		{
			name:          reconcileCommandName,
			args:          []string{workloadCommandName, reconcileCommandName, "--timeout=1ms"},
			expectedError: "create flux reconciler",
			writeConfig:   writeFluxReconcileKsailConfig,
		},
		{
			name: pushCommandName,
			args: []string{
				workloadCommandName,
				pushCommandName,
				"oci://example.com:5000/test:dev",
			},
			expectedError: "no manifest files found in source directory",
			writeConfig:   writeValidKsailConfig,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Intentionally not parallel: this test exercises config discovery via t.Chdir.
			var out bytes.Buffer

			tempDir := t.TempDir()
			testCase.writeConfig(t, tempDir)

			workloadConfigDiscoveryMu.Lock()
			t.Cleanup(workloadConfigDiscoveryMu.Unlock)

			t.Chdir(tempDir)

			root := cmd.NewRootCmd("test", "test", "test")
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs(testCase.args)

			err := root.Execute()
			require.ErrorContains(
				t,
				err,
				testCase.expectedError,
				"expected workload %s handler to return proper error",
				testCase.name,
			)

			actual := out.String()
			require.Contains(t, actual, "config loaded")
			require.NotContains(t, actual, "coming soon")
			require.NotContains(t, actual, "ℹ")
		})
	}
}

func TestNewWorkloadCmdRunETriggersHelp(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	command := workload.NewWorkloadCmd()
	command.SetOut(&out)
	command.SetErr(&out)

	err := command.Execute()
	require.NoError(t, err)

	snaps.MatchSnapshot(t, normalizeHomePaths(out.String()))
}

// TestTopologicalSortKustomizations tests the topological sort of Flux Kustomizations.
//
//nolint:funlen // Table-driven test with comprehensive cases
func TestTopologicalSortKustomizations(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    []flux.KustomizationInfo
		expected []string // expected order of names
	}{
		{
			name:     "empty list",
			input:    nil,
			expected: nil,
		},
		{
			name: "single kustomization",
			input: []flux.KustomizationInfo{
				{Name: "apps", Path: "./apps"},
			},
			expected: []string{"apps"},
		},
		{
			name: "no dependencies preserves order",
			input: []flux.KustomizationInfo{
				{Name: "apps", Path: "./apps"},
				{Name: "infra", Path: "./infra"},
				{Name: "monitoring", Path: "./monitoring"},
			},
			expected: []string{"apps", "infra", "monitoring"},
		},
		{
			name: "linear dependency chain",
			input: []flux.KustomizationInfo{
				{Name: "apps", Path: "./apps", DependsOn: []string{"infra"}},
				{Name: "infra", Path: "./infra", DependsOn: []string{"flux-system"}},
				{Name: "flux-system", Path: "./"},
			},
			expected: []string{"flux-system", "infra", "apps"},
		},
		{
			name: "diamond dependencies",
			input: []flux.KustomizationInfo{
				{Name: "apps", Path: "./apps", DependsOn: []string{"infra", "config"}},
				{Name: "infra", Path: "./infra", DependsOn: []string{"flux-system"}},
				{Name: "config", Path: "./config", DependsOn: []string{"flux-system"}},
				{Name: "flux-system", Path: "./"},
			},
			expected: []string{"flux-system", "infra", "config", "apps"},
		},
		{
			name: "dependency on nonexistent kustomization ignored",
			input: []flux.KustomizationInfo{
				{Name: "apps", Path: "./apps", DependsOn: []string{"nonexistent"}},
				{Name: "infra", Path: "./infra"},
			},
			expected: []string{"apps", "infra"},
		},
		{
			name: "cycle protection appends remaining",
			input: []flux.KustomizationInfo{
				{Name: "a", Path: "./a", DependsOn: []string{"b"}},
				{Name: "b", Path: "./b", DependsOn: []string{"a"}},
				{Name: "c", Path: "./c"},
			},
			expected: []string{"c", "a", "b"},
		},
		{
			name: "duplicate dependencies are de-duplicated",
			input: []flux.KustomizationInfo{
				{Name: "apps", Path: "./apps", DependsOn: []string{"infra", "infra"}},
				{Name: "infra", Path: "./infra"},
			},
			expected: []string{"infra", "apps"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sorted := workload.ExportTopologicalSortKustomizations(testCase.input)

			if testCase.expected == nil {
				assert.Empty(t, sorted)

				return
			}

			names := make([]string, len(sorted))
			for i, ks := range sorted {
				names[i] = ks.Name
			}

			assert.Equal(t, testCase.expected, names)
		})
	}
}

func TestOutputPlain(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	images := []string{"nginx:latest", "redis:7", "postgres:16"}
	err := workload.ExportOutputPlain(cmd, images)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "nginx:latest")
	assert.Contains(t, output, "redis:7")
	assert.Contains(t, output, "postgres:16")
}

func TestOutputJSON(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	images := []string{"nginx:latest", "redis:7"}
	err := workload.ExportOutputJSON(cmd, images)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "nginx:latest")
	assert.Contains(t, output, "redis:7")
	assert.Contains(t, output, "[")
}

// Sentinel errors used by the failedKustomizations and poll fail-fast tests.
// Using package-level variables satisfies the err113 (goerr113) linter rule
// that forbids inline errors.New() calls in non-test code and test assertions.
var (
	errUpstreamValidation    = errors.New("upstream validation error")
	errFluxSystemReconcile   = errors.New("flux-system: reconciliation failed")
	errInfraReconcile        = errors.New("infra: reconciliation failed")
	errInfraPermanentFailure = errors.New("infra: permanent failure")
	errGenericFailed         = errors.New("failed")
)

// =============================================================================
// failedKustomizations — cascade fail-fast tracker
// =============================================================================

func TestFailedKustomizationsCheckDependenciesNoneRecorded(t *testing.T) {
	t.Parallel()

	var tracker workload.ExportFailedKustomizations

	err := workload.ExportCheckKustomizationDependencies(&tracker, []string{"infra", "config"})

	require.NoError(t, err)
}

func TestFailedKustomizationsCheckDependenciesDirectFailure(t *testing.T) {
	t.Parallel()

	var tracker workload.ExportFailedKustomizations

	workload.ExportRecordKustomizationFailure(&tracker, "infra", errUpstreamValidation)

	err := workload.ExportCheckKustomizationDependencies(&tracker, []string{"infra"})

	require.Error(t, err)
	// The cascade error names the direct dependency and wraps the shared
	// sentinel, but deliberately does NOT embed the upstream error — repeating it
	// at every level is what made the output unreadable.
	require.ErrorIs(t, err, workload.ExportErrDependencyBlocked())
	require.NotErrorIs(t, err, errUpstreamValidation)
	assert.Contains(t, err.Error(), "infra")
}

func TestFailedKustomizationsCheckDependenciesTransitivePropagation(t *testing.T) {
	t.Parallel()

	// Simulate: flux-system fails → infra detects it and records itself failed
	// → apps detects infra failed.
	var tracker workload.ExportFailedKustomizations

	workload.ExportRecordKustomizationFailure(&tracker, "flux-system", errFluxSystemReconcile)

	// infra depends on flux-system: it detects the failure…
	infraDepErr := workload.ExportCheckKustomizationDependencies(&tracker, []string{"flux-system"})
	require.Error(t, infraDepErr)

	// …and records itself as failed (cascade).
	workload.ExportRecordKustomizationFailure(&tracker, "infra", infraDepErr)

	// apps depends on infra: it should also fail promptly, naming its direct
	// dependency ("infra") without dragging along the whole upstream chain.
	appDepErr := workload.ExportCheckKustomizationDependencies(&tracker, []string{"infra"})

	require.Error(t, appDepErr)
	require.ErrorIs(t, appDepErr, workload.ExportErrDependencyBlocked())
	assert.Contains(t, appDepErr.Error(), "infra")
	assert.NotContains(t, appDepErr.Error(), "flux-system")
}

func TestFailedKustomizationsCheckDependenciesNoDeps(t *testing.T) {
	t.Parallel()

	var tracker workload.ExportFailedKustomizations

	workload.ExportRecordKustomizationFailure(&tracker, "infra", errGenericFailed)

	// A kustomization with no dependencies is unaffected by tracked failures.
	err := workload.ExportCheckKustomizationDependencies(&tracker, nil)

	require.NoError(t, err)
}

func TestPollUntilKustomizationReadyFailsFastOnDependencyFailure(t *testing.T) {
	t.Parallel()

	var tracker workload.ExportFailedKustomizations

	workload.ExportRecordKustomizationFailure(&tracker, "infra", errInfraReconcile)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// Pass a nil reconciler — the dependency check must short-circuit before any
	// API call is made, so the nil pointer is never dereferenced.
	err := workload.ExportPollUntilKustomizationReady(
		ctx,
		nil, // *flux.Reconciler — must not be called
		"apps",
		[]string{"infra"},
		&tracker,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "infra")
}

func TestPollUntilKustomizationReadyRecordsCascadeFailure(t *testing.T) {
	t.Parallel()

	var tracker workload.ExportFailedKustomizations

	workload.ExportRecordKustomizationFailure(&tracker, "infra", errInfraPermanentFailure)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// "apps" depends on "infra" (already failed).
	_ = workload.ExportPollUntilKustomizationReady(
		ctx, nil, "apps", []string{"infra"}, &tracker,
	)

	// "apps" itself should now be recorded as failed, enabling further cascade.
	err := workload.ExportCheckKustomizationDependencies(&tracker, []string{"apps"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "apps")
}

func TestIsAggregatedReconcileError(t *testing.T) {
	t.Parallel()

	// The aggregated kustomization/application errors are collapsed to the
	// diagnostics summary; other errors keep their original message.
	kustErr := fmt.Errorf(
		"%w: infra: permanent failure",
		workload.ExportErrKustomizationReconcile(),
	)
	retried := fmt.Errorf("failed after 3 attempts: %w", kustErr)

	assert.True(t, workload.ExportIsAggregatedReconcileError(kustErr))
	assert.True(t, workload.ExportIsAggregatedReconcileError(retried))
	assert.False(t, workload.ExportIsAggregatedReconcileError(errGenericFailed))
}
