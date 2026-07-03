package workload_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeConfigMap writes a kubeconform-valid ConfigMap in the given namespace into
// dir, so CEL rules — not kubeconform — decide whether validation passes.
func writeConfigMap(t *testing.T, dir, namespace string) {
	t.Helper()

	content := `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: ` + namespace + `
data:
  key: value
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configmap.yaml"), []byte(content), 0o600))
}

// writeRulesFile writes a CEL rules file into its own temp directory (kept
// outside any validated path so it is not itself picked up as a manifest) and
// returns its path.
func writeRulesFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// requireProdNamespaceRule is an error-severity rule the "default"-namespace
// ConfigMap fixture violates.
const requireProdNamespaceRule = `rules:
  - name: require-prod-namespace
    expression: 'object.metadata.namespace == "prod"'
    message: "resources must be in the prod namespace"
    severity: error
`

func TestValidateCELErrorViolationFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeConfigMap(t, dir, "default")
	rules := writeRulesFile(t, requireProdNamespaceRule)

	_, err := runValidate(t, dir, "--rules", rules)
	require.Error(t, err, "an error-severity CEL violation should fail validation")
	require.ErrorContains(t, err, "require-prod-namespace", "the failure should name the rule")
	require.ErrorContains(
		t, err, "ConfigMap/default/app-config",
		"the failure should be attributed to the offending resource identity",
	)
	require.ErrorContains(
		t, err, "resources must be in the prod namespace",
		"the rule message should surface",
	)
}

func TestValidateCELWarningViolationDoesNotFail(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeConfigMap(t, dir, "default")
	rules := writeRulesFile(t, `rules:
  - name: prefer-prod-namespace
    expression: 'object.metadata.namespace == "prod"'
    message: "prefer the prod namespace"
    severity: warning
`)

	out, err := runValidate(t, dir, "--rules", rules)
	require.NoError(t, err, "a warning-severity CEL violation must not fail validation")
	assert.Contains(t, out, "CEL rule warning", "a warning violation should be reported")
	assert.Contains(t, out, "prefer-prod-namespace", "the warning should name the rule")
}

func TestValidateCELRulePasses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeConfigMap(t, dir, "prod")
	rules := writeRulesFile(t, requireProdNamespaceRule)

	_, err := runValidate(t, dir, "--rules", rules)
	require.NoError(t, err, "a satisfied CEL rule should pass validation")
}

func TestValidateWithoutRulesUnchanged(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeConfigMap(t, dir, "default")

	_, err := runValidate(t, dir)
	require.NoError(t, err, "without --rules, validation should be unaffected by CEL")
}

func TestValidateCELBadRulesFileFailsFast(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeConfigMap(t, dir, "default")
	// A non-boolean expression fails to compile.
	rules := writeRulesFile(t, `rules:
  - name: not-a-bool
    expression: 'object.metadata.name'
`)

	_, err := runValidate(t, dir, "--rules", rules)
	require.Error(t, err, "a non-compiling rules file should fail fast")
	require.ErrorContains(t, err, "compile CEL rules", "the error should point at rule compilation")
}

func TestValidateCELMissingRulesFileFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeConfigMap(t, dir, "default")

	_, err := runValidate(t, dir, "--rules", filepath.Join(dir, "does-not-exist.yaml"))
	require.Error(t, err, "a missing rules file should fail fast")
	require.ErrorContains(t, err, "rules file", "the error should reference the rules file")
}

func TestValidateCELErrorViolationOnRenderedManifest(t *testing.T) {
	t.Parallel()

	// A HelmRelease that renders a ConfigMap: the rule is evaluated against the
	// rendered output, not the HelmRelease CR, exercising the kustomization path.
	dir := writeHelmReleaseKustomization(t, localChartURL(t, "validchart"))
	rules := writeRulesFile(t, `rules:
  - name: forbid-configmaps
    expression: 'object.kind != "ConfigMap"'
    message: "ConfigMaps are not allowed"
    severity: error
`)

	_, err := runValidate(t, dir, "--skip-kinds", "OCIRepository", "--rules", rules)
	require.Error(t, err, "a CEL rule should evaluate the rendered manifests")
	require.ErrorContains(
		t, err, "forbid-configmaps", "the rendered-output violation should name the rule",
	)
}

func TestDocumentIdentityFromObject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		obj  map[string]any
		want string
	}{
		{
			name: "namespaced",
			obj: map[string]any{
				"kind":     "ConfigMap",
				"metadata": map[string]any{"name": "cfg", "namespace": "prod"},
			},
			want: "ConfigMap/prod/cfg",
		},
		{
			name: "cluster-scoped",
			obj: map[string]any{
				"kind":     "ClusterRole",
				"metadata": map[string]any{"name": "admin"},
			},
			want: "ClusterRole/admin",
		},
		{
			name: "missing kind",
			obj:  map[string]any{"metadata": map[string]any{"name": "cfg"}},
			want: "",
		},
		{
			name: "missing name",
			obj:  map[string]any{"kind": "ConfigMap", "metadata": map[string]any{}},
			want: "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, workload.ExportDocumentIdentityFromObject(testCase.obj))
		})
	}
}

func TestDecodeDocumentObjectSkipsNonMappings(t *testing.T) {
	t.Parallel()

	_, emptyOK := workload.ExportDecodeDocumentObject([]byte("   \n"))
	assert.False(t, emptyOK, "an empty document should be skipped")

	_, listOK := workload.ExportDecodeDocumentObject([]byte("- a\n- b\n"))
	assert.False(t, listOK, "a bare list document has no object and should be skipped")

	obj, mapOK := workload.ExportDecodeDocumentObject(
		[]byte("kind: ConfigMap\nmetadata:\n  name: cfg\n"),
	)
	require.True(t, mapOK, "a mapping document should decode")
	assert.Equal(t, "ConfigMap", obj["kind"])
}
