package workload_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
