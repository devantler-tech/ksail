package workload_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeRenderedCRDKustomization writes a kustomization wrapping an OCIRepository +
// HelmRelease that renders the "crdchart" fixture (which ships the widget CRD
// under templates/, not present anywhere in this directory) plus a Widget custom
// resource, and returns the directory. The widget CRD is therefore discoverable
// only by rendering the chart, never by walking this directory's raw tree.
func writeRenderedCRDKustomization(t *testing.T, resource string) string {
	t.Helper()

	dir := writeHelmReleaseKustomization(t, localChartURL(t, "crdchart"))

	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(dir, "kustomization.yaml"),
			[]byte(`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ocirepository.yaml
  - helmrelease.yaml
  - widget.yaml
`),
			0o600,
		),
	)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "widget.yaml"), []byte(resource), 0o600))

	return dir
}

// widgetCRDManifest defines a CRD kind absent from the built-in schemas and the
// CRDs-catalog, whose spec forbids extra properties so an invalid instance is
// catchable once the CRD-derived schema is in play.
const widgetCRDManifest = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
    plural: widgets
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                size:
                  type: integer
              required: [size]
              additionalProperties: false
          required: [spec]
`

const invalidWidget = `apiVersion: example.com/v1
kind: Widget
metadata:
  name: widget
spec:
  size: 3
  bogus: true
`

const validWidget = `apiVersion: example.com/v1
kind: Widget
metadata:
  name: widget
spec:
  size: 3
`

// writeCRDTree writes the widget CRD plus one custom resource into a fresh temp
// directory and returns it.
func writeCRDTree(t *testing.T, resource string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(filepath.Join(dir, "crd.yaml"), []byte(widgetCRDManifest), 0o600),
	)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cr.yaml"), []byte(resource), 0o600))

	return dir
}

// TestValidateIncludeCRDSchemas_CatchesInvalidCR is the flag-on path: with
// --include-crd-schemas, a custom resource is validated against the schema derived
// from its CRD in the tree, so an invalid instance fails.
func TestValidateIncludeCRDSchemas_CatchesInvalidCR(t *testing.T) {
	t.Parallel()

	dir := writeCRDTree(t, invalidWidget)

	_, err := runValidate(t, dir, "--include-crd-schemas")
	require.Error(t, err, "an invalid CR should be caught against its CRD-derived schema")
}

// TestValidateIncludeCRDSchemas_PassesValidCR is the flag-on happy path: a valid
// custom resource passes against its CRD-derived schema.
func TestValidateIncludeCRDSchemas_PassesValidCR(t *testing.T) {
	t.Parallel()

	dir := writeCRDTree(t, validWidget)

	_, err := runValidate(t, dir, "--include-crd-schemas")
	require.NoError(t, err, "a valid CR should pass against its CRD-derived schema")
}

// TestValidateWithoutCRDSchemas_SkipsCR is the flag-off path: without the flag the
// custom resource has no schema and is skipped (default behavior is unchanged), so
// even an invalid instance passes.
func TestValidateWithoutCRDSchemas_SkipsCR(t *testing.T) {
	t.Parallel()

	dir := writeCRDTree(t, invalidWidget)

	_, err := runValidate(t, dir)
	require.NoError(t, err, "without --include-crd-schemas the CR is skipped, so it must pass")
}

// TestValidateIncludeCRDSchemas_CatchesInvalidRenderedCR proves the headline
// behavior added for #5906: with --include-crd-schemas, a CRD that is only
// present in a HelmRelease's RENDERED output (the widget CRD lives under
// crdchart/templates/, never copied into this kustomization's own tree) is still
// discovered, so an invalid custom resource of that kind is caught — not silently
// skipped as it would be with only the raw-tree walk from #5905.
func TestValidateIncludeCRDSchemas_CatchesInvalidRenderedCR(t *testing.T) {
	t.Parallel()

	dir := writeRenderedCRDKustomization(t, invalidWidget)

	_, err := runValidate(t, dir, "--include-crd-schemas", "--skip-kinds", "OCIRepository")
	require.Error(
		t,
		err,
		"an invalid CR should be caught against a schema derived from a rendered CRD",
	)
}

// TestValidateIncludeCRDSchemas_PassesValidRenderedCR is the rendered-CRD happy
// path: a valid custom resource of the rendered-only kind passes.
func TestValidateIncludeCRDSchemas_PassesValidRenderedCR(t *testing.T) {
	t.Parallel()

	dir := writeRenderedCRDKustomization(t, validWidget)

	_, err := runValidate(t, dir, "--include-crd-schemas", "--skip-kinds", "OCIRepository")
	require.NoError(t, err, "a valid CR should pass against a schema derived from a rendered CRD")
}

// TestValidateIncludeCRDSchemas_SkipHelmRenderIgnoresRenderedCR proves the two
// flags compose as documented: --skip-helm-render disables rendering entirely, so
// a CRD only present in rendered output is invisible even with
// --include-crd-schemas — the invalid CR is skipped rather than caught, and the
// HelmRelease CR itself (not a rendered ConfigMap/CRD) is what gets validated.
func TestValidateIncludeCRDSchemas_SkipHelmRenderIgnoresRenderedCR(t *testing.T) {
	t.Parallel()

	dir := writeRenderedCRDKustomization(t, invalidWidget)

	_, err := runValidate(
		t, dir, "--include-crd-schemas", "--skip-helm-render", "--skip-kinds", "OCIRepository",
	)
	require.NoError(
		t, err,
		"without rendering, the rendered-only CRD is undiscoverable so the invalid CR is skipped",
	)
}
