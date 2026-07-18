package workload_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// localChartURL returns the absolute path to a testdata chart directory. The
// path is used as an OCIRepository spec.url so that the in-process Helm client
// loads the chart from disk — exercising the real resolver→Helm→render pipeline
// offline (Helm treats a non-oci:// reference as a local chart path).
func localChartURL(t *testing.T, chart string) string {
	t.Helper()

	abs, err := filepath.Abs(filepath.Join("testdata", "charts", chart))
	require.NoError(t, err)

	return abs
}

// writeHelmReleaseKustomization writes a kustomization that wraps an
// OCIRepository (pointing at chartURL) and a HelmRelease referencing it, and
// returns the directory.
func writeHelmReleaseKustomization(t *testing.T, chartURL string) string {
	t.Helper()

	dir := t.TempDir()
	writeKustomizationFiles(t, dir, chartURL)

	return dir
}

// writeKustomizationFiles writes the OCIRepository + HelmRelease kustomization
// into an existing directory.
func writeKustomizationFiles(t *testing.T, dir, chartURL string) {
	t.Helper()

	files := map[string]string{
		"kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ocirepository.yaml
  - helmrelease.yaml
`,
		"ocirepository.yaml": `apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: chart
  namespace: flux-system
spec:
  interval: 5m
  url: ` + chartURL + `
`,
		"helmrelease.yaml": `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: app
  namespace: flux-system
spec:
  interval: 5m
  chartRef:
    kind: OCIRepository
    name: chart
`,
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
}

func runValidate(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := workload.NewValidateCmd()
	cmd.SetArgs(args)

	var output bytes.Buffer

	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cmd.Execute()

	return output.String(), err
}

// TestValidateRendersHelmReleaseFromLocalChart verifies a HelmRelease whose
// chart renders valid manifests passes validation (the rendered output is what
// gets validated). OCIRepository is skipped because its spec.url is a local path
// in this fixture, not a real oci:// reference.
func TestValidateRendersHelmReleaseFromLocalChart(t *testing.T) {
	t.Parallel()

	dir := writeHelmReleaseKustomization(t, localChartURL(t, "validchart"))

	_, err := runValidate(t, dir, "--skip-kinds", "OCIRepository")
	require.NoError(t, err, "rendered valid chart output should pass validation")
}

// TestValidateCatchesInvalidRenderedManifest verifies that a HelmRelease whose
// chart renders a schema-invalid manifest fails validation — the headline of
// #5344 — and that the failure is attributed back to the originating HelmRelease
// layer (from the render provenance) so a multi-layer render points at the source.
func TestValidateCatchesInvalidRenderedManifest(t *testing.T) {
	t.Parallel()

	dir := writeHelmReleaseKustomization(t, localChartURL(t, "invalidchart"))

	_, err := runValidate(t, dir, "--skip-kinds", "OCIRepository")
	require.Error(t, err, "rendered invalid chart output should fail validation")
	// The invalid ConfigMap is rendered from the HelmRelease flux-system/app, so the
	// failure carries its source layer (SourceHelmRelease is "namespace/name").
	assert.ErrorContains(
		t,
		err,
		"(from HelmRelease flux-system/app)",
		"a rendered failure should be attributed to its HelmRelease layer",
	)
}

// TestValidateSkipHelmRenderValidatesCRAsIs verifies --skip-helm-render disables
// rendering: the same invalid chart is not rendered, so only the (valid)
// HelmRelease CR is validated and the run passes.
func TestValidateSkipHelmRenderValidatesCRAsIs(t *testing.T) {
	t.Parallel()

	dir := writeHelmReleaseKustomization(t, localChartURL(t, "invalidchart"))

	_, err := runValidate(t, dir, "--skip-helm-render", "--skip-kinds", "OCIRepository")
	require.NoError(t, err, "with --skip-helm-render the invalid chart is not rendered")
}

// TestValidateDegradesWhenChartUnreachable verifies graceful degradation: when a
// chart cannot be resolved offline the HelmRelease CR is validated instead, a
// warning is emitted, and the run does not hard-fail.
func TestValidateDegradesWhenChartUnreachable(t *testing.T) {
	t.Parallel()

	// 127.0.0.1:1 refuses connections immediately, so the OCI pull fails fast.
	dir := writeHelmReleaseKustomization(t, "oci://127.0.0.1:1/charts/missing")

	output, err := runValidate(t, dir)
	require.NoError(t, err, "an unreachable chart must degrade, not fail the run")
	assert.Contains(t, output, "skipped Helm render", "degradation should warn the user")
}

// TestValidateWarnsOnUnresolvableValuesFrom verifies the render-fidelity honesty
// contract end-to-end: a HelmRelease whose non-optional valuesFrom references a
// ConfigMap absent from the offline stream (typically cluster-managed) still
// renders and passes, but the user is warned that values were incomplete — not
// the misleading "skipped Helm render" message, since the chart DID render.
func TestValidateWarnsOnUnresolvableValuesFrom(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	chartURL := localChartURL(t, "validchart")
	files := map[string]string{
		"kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ocirepository.yaml
  - helmrelease.yaml
`,
		"ocirepository.yaml": `apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: chart
  namespace: flux-system
spec:
  interval: 5m
  url: ` + chartURL + `
`,
		"helmrelease.yaml": `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: app
  namespace: flux-system
spec:
  interval: 5m
  chartRef:
    kind: OCIRepository
    name: chart
  valuesFrom:
    - kind: ConfigMap
      name: cluster-settings
`,
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	output, err := runValidate(t, dir, "--skip-kinds", "OCIRepository")
	require.NoError(t, err, "an unresolvable valuesFrom must warn, not fail the run")
	assert.Contains(t, output, "incomplete values", "the user should be warned values were partial")
	assert.Contains(
		t,
		output,
		"cluster-settings",
		"the warning should name the unresolved reference",
	)
	assert.NotContains(
		t,
		output,
		"skipped Helm render",
		"the chart did render, so the skipped-render message must not appear",
	)
}

// TestValidateSingleHelmReleaseFileStaysGreen guards the ksail-test-gen-smoke CI
// canary: validating a single HelmRelease file (whose source is not present)
// must not attempt rendering and must pass as a CR-schema check.
func TestValidateSingleHelmReleaseFileStaysGreen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "helmrelease.yaml")
	content := `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: test-hr
  namespace: flux-system
spec:
  interval: 5m
  chart:
    spec:
      chart: test
      sourceRef:
        kind: HelmRepository
        name: test
`
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))

	_, err := runValidate(t, file)
	require.NoError(t, err, "a single HelmRelease file must validate as a CR (no render)")
}

// TestValidateRendersConcurrentlyNoRace renders several kustomizations in one run
// (validate fans out across kustomizations) to guard the concurrency contract:
// each render constructs its own Helm template client, so there is no shared
// helm.action.Configuration. Run with -race to catch a regression that shares one.
func TestValidateRendersConcurrentlyNoRace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	chartURL := localChartURL(t, "validchart")

	for _, app := range []string{"app1", "app2", "app3", "app4"} {
		appDir := filepath.Join(root, app)
		require.NoError(t, os.MkdirAll(appDir, 0o750))
		writeKustomizationFiles(t, appDir, chartURL)
	}

	_, err := runValidate(t, root, "--skip-kinds", "OCIRepository")
	require.NoError(t, err, "concurrent rendering of multiple kustomizations must succeed")
}

// TestValidateGitRepositorySourceSkipsSilently verifies a HelmRelease whose chart
// comes from a GitRepository (not renderable offline yet) degrades *silently*:
// the run passes (the CR is validated as-is) and no warning is emitted, so normal
// repos that mix GitRepository charts are not spammed.
func TestValidateGitRepositorySourceSkipsSilently(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	files := map[string]string{
		"kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - helmrelease.yaml
`,
		"helmrelease.yaml": `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: app
  namespace: flux-system
spec:
  interval: 5m
  chart:
    spec:
      chart: ./charts/app
      sourceRef:
        kind: GitRepository
        name: repo
`,
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	output, err := runValidate(t, dir)
	require.NoError(t, err, "a GitRepository-sourced HelmRelease must degrade silently, not fail")
	assert.NotContains(t, output, "skipped Helm render", "GitRepository sources skip silently")
}

// writeWidgetKustomization writes a kustomization containing a single CR of a
// CRD kind absent from both the built-in schemas and the CRDs-catalog, plus a
// schema file for it, and returns (kustomization dir, schema-location template).
func writeWidgetKustomization(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()

	const kustomization = `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - widget.yaml
`
	// Violates the schema below (additionalProperties: false rejects "bogus").
	const (
		widgetCR = `apiVersion: example.com/v1
kind: WidgetThing
metadata:
  name: widget
spec:
  size: 3
  bogus: true
`
		widgetSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "spec": {
      "type": "object",
      "properties": {"size": {"type": "integer"}},
      "required": ["size"],
      "additionalProperties": false
    }
  },
  "required": ["spec"]
}`
	)

	require.NoError(
		t,
		os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte(kustomization), 0o600),
	)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "widget.yaml"), []byte(widgetCR), 0o600))

	schemaDir := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(filepath.Join(schemaDir, "widgetthing_v1.json"), []byte(widgetSchema), 0o600),
	)

	return dir, filepath.Join(schemaDir, "{{.ResourceKind}}_{{.ResourceAPIVersion}}.json")
}

// TestValidateSchemaLocationCatchesCatalogAbsentCRD verifies that
// --schema-location lets validate catch a CRD that is absent from the
// CRDs-catalog (which would otherwise be ignored) — the #5344 Phase 3a gap.
func TestValidateSchemaLocationCatchesCatalogAbsentCRD(t *testing.T) {
	t.Parallel()

	dir, schemaLoc := writeWidgetKustomization(t)

	_, err := runValidate(t, dir, "--schema-location", schemaLoc)
	require.Error(t, err, "a catalog-absent CRD must be caught when its schema is supplied")
}

// TestValidateWithoutSchemaLocationIgnoresCatalogAbsentCRD is the contrast: the
// same invalid CR passes when no schema is supplied (the kind is unknown and
// ignored), proving --schema-location is what enables the catch above.
func TestValidateWithoutSchemaLocationIgnoresCatalogAbsentCRD(t *testing.T) {
	t.Parallel()

	dir, _ := writeWidgetKustomization(t)

	_, err := runValidate(t, dir)
	require.NoError(t, err, "a catalog-absent CRD is ignored without a supplied schema")
}
