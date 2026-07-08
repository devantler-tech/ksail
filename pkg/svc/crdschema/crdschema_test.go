package crdschema_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/devantler-tech/ksail/v7/pkg/svc/crdschema"
	"github.com/stretchr/testify/require"
)

// widgetCRD is a CRD manifest for a fictional kind absent from both the built-in
// Kubernetes schemas and the CRDs-catalog. Its spec forbids extra properties so an
// invalid instance is catchable.
const widgetCRD = `apiVersion: apiextensions.k8s.io/v1
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

const validWidgetCR = `apiVersion: example.com/v1
kind: Widget
metadata:
  name: widget
spec:
  size: 3
`

const invalidWidgetCR = `apiVersion: example.com/v1
kind: Widget
metadata:
  name: widget
spec:
  size: 3
  bogus: true
`

// writeTree writes each named file under a fresh temp directory and returns it.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()

	for name, content := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	return root
}

func TestMaterialize_WritesSchemaPerVersion(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{"crds/widget.yaml": widgetCRD})
	dest := t.TempDir()

	result, err := crdschema.Materialize(root, dest)
	require.NoError(t, err)
	require.Equal(t, 1, result.Written)
	require.Empty(t, result.Warnings)

	schemaPath := filepath.Join(dest, "example.com", "widget_v1.json")
	raw, err := os.ReadFile(schemaPath) //nolint:gosec // test-controlled path
	require.NoError(t, err, "expected a schema written at %s", schemaPath)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))

	// The standard object fields are injected and the CRD's own spec is preserved.
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, props, "apiVersion")
	require.Contains(t, props, "kind")
	require.Contains(t, props, "metadata")
	require.Contains(t, props, "spec")
}

func TestMaterialize_MultipleVersions(t *testing.T) {
	t.Parallel()

	const twoVersionCRD = `apiVersion: apiextensions.k8s.io/v1
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
      schema:
        openAPIV3Schema:
          type: object
    - name: v2
      schema:
        openAPIV3Schema:
          type: object
`

	root := writeTree(t, map[string]string{"widget.yaml": twoVersionCRD})
	dest := t.TempDir()

	result, err := crdschema.Materialize(root, dest)
	require.NoError(t, err)
	require.Equal(t, 2, result.Written)

	require.FileExists(t, filepath.Join(dest, "example.com", "widget_v1.json"))
	require.FileExists(t, filepath.Join(dest, "example.com", "widget_v2.json"))
}

func TestMaterialize_VersionWithoutSchemaWarns(t *testing.T) {
	t.Parallel()

	const mixedCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
    plural: widgets
  versions:
    - name: v1alpha1
    - name: v1
      schema:
        openAPIV3Schema:
          type: object
`

	root := writeTree(t, map[string]string{"widget.yaml": mixedCRD})
	dest := t.TempDir()

	result, err := crdschema.Materialize(root, dest)
	require.NoError(t, err)
	require.Equal(t, 1, result.Written)
	require.Len(t, result.Warnings, 1)
	require.Contains(t, result.Warnings[0].String(), "v1alpha1")
	require.Contains(t, result.Warnings[0].String(), "widgets.example.com")
}

func TestMaterialize_IgnoresNonCRDDocuments(t *testing.T) {
	t.Parallel()

	const deploymentAndCRD = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
---
` + widgetCRD

	root := writeTree(t, map[string]string{"manifests.yaml": deploymentAndCRD})
	dest := t.TempDir()

	result, err := crdschema.Materialize(root, dest)
	require.NoError(t, err)
	require.Equal(t, 1, result.Written)
	require.FileExists(t, filepath.Join(dest, "example.com", "widget_v1.json"))
}

func TestMaterialize_MalformedCRDWarnsWithoutError(t *testing.T) {
	t.Parallel()

	const noGroupCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: broken.example.com
spec:
  names:
    kind: Broken
  versions:
    - name: v1
      schema:
        openAPIV3Schema:
          type: object
`

	root := writeTree(t, map[string]string{"broken.yaml": noGroupCRD})
	dest := t.TempDir()

	result, err := crdschema.Materialize(root, dest)
	require.NoError(t, err)
	require.Equal(t, 0, result.Written)
	require.Len(t, result.Warnings, 1)
	require.Contains(t, result.Warnings[0].Reason, "spec.group")
}

func TestMaterialize_RejectsPathTraversalInCRDFields(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		crd string
		// escaped is a path (relative to dest) that must NOT be created — the
		// location the schema would land at if the traversal guard were absent.
		escaped string
	}{
		"group escapes dest": {
			crd: `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: evil.example.com
spec:
  group: "../escape"
  names:
    kind: Evil
  versions:
    - name: v1
      schema:
        openAPIV3Schema:
          type: object
`,
			escaped: filepath.Join("..", "escape", "evil_v1.json"),
		},
		"version name escapes dest": {
			crd: `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: evil.example.com
spec:
  group: example.com
  names:
    kind: Evil
  versions:
    - name: "../../evil"
      schema:
        openAPIV3Schema:
          type: object
`,
			escaped: filepath.Join("example.com", "..", "..", "evil_../../evil.json"),
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := writeTree(t, map[string]string{"evil.yaml": testCase.crd})
			dest := t.TempDir()

			result, err := crdschema.Materialize(root, dest)
			require.NoError(t, err)
			require.Equal(t, 0, result.Written)
			require.Len(t, result.Warnings, 1)
			require.Contains(t, result.Warnings[0].Reason, "safe path component")
			require.NoFileExists(t, filepath.Join(dest, testCase.escaped))
		})
	}
}

func TestMaterialize_NonexistentRootErrors(t *testing.T) {
	t.Parallel()

	_, err := crdschema.Materialize(filepath.Join(t.TempDir(), "does-not-exist"), t.TempDir())
	require.Error(t, err)
}

func TestSchemaLocation(t *testing.T) {
	t.Parallel()

	loc := crdschema.SchemaLocation("/tmp/schemas")
	require.Equal(
		t,
		filepath.Join(
			"/tmp/schemas",
			"{{.Group}}",
			"{{.ResourceKind}}_{{.ResourceAPIVersion}}.json",
		),
		loc,
	)
}

// TestMaterialize_DerivedSchemaValidatesViaKubeconform proves the end-to-end payoff:
// a schema derived from a real CRD manifest, fed to kubeconform via the location
// SchemaLocation returns, passes a valid custom resource and catches an invalid one
// (flag-on behavior) — while without the location the invalid CR is ignored
// (flag-off behavior). Runs offline: the catalog registry errors and kubeconform
// falls through to the local schema.
func TestMaterialize_DerivedSchemaValidatesViaKubeconform(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{"crds/widget.yaml": widgetCRD})
	dest := t.TempDir()

	result, err := crdschema.Materialize(root, dest)
	require.NoError(t, err)
	require.Equal(t, 1, result.Written)

	client := kubeconform.NewClient()
	ctx := context.Background()
	location := crdschema.SchemaLocation(dest)

	// Flag-on: a valid CR passes against the derived schema...
	err = client.ValidateBytes(
		ctx,
		"widget.yaml",
		[]byte(validWidgetCR),
		&kubeconform.ValidationOptions{
			IgnoreMissingSchemas: false,
			SchemaLocations:      []string{location},
		},
	)
	require.NoError(t, err, "valid CR should pass against the derived CRD schema")

	// ...and an invalid CR is caught.
	err = client.ValidateBytes(
		ctx,
		"widget.yaml",
		[]byte(invalidWidgetCR),
		&kubeconform.ValidationOptions{
			IgnoreMissingSchemas: false,
			SchemaLocations:      []string{location},
		},
	)
	require.Error(t, err, "invalid CR should fail against the derived CRD schema")

	// Flag-off: without the derived schema the kind has no schema and is ignored.
	err = client.ValidateBytes(
		ctx,
		"widget.yaml",
		[]byte(invalidWidgetCR),
		&kubeconform.ValidationOptions{
			IgnoreMissingSchemas: true,
		},
	)
	require.NoError(t, err, "without the derived schema the CR should be skipped")
}

// TestMaterializeBytes_WritesSchemaPerVersion proves MaterializeBytes finds a CRD
// in a pre-rendered manifest stream (a []byte, not a file on disk) exactly like
// Materialize finds one by walking a file tree — the two entry points share the
// same per-document extraction, just fed from a different source.
func TestMaterializeBytes_WritesSchemaPerVersion(t *testing.T) {
	t.Parallel()

	dest := t.TempDir()

	result, err := crdschema.MaterializeBytes(
		[]byte(widgetCRD),
		"helmrelease/widgets (rendered)",
		dest,
	)
	require.NoError(t, err)
	require.Equal(t, 1, result.Written)
	require.Empty(t, result.Warnings)
	require.FileExists(t, filepath.Join(dest, "example.com", "widget_v1.json"))
}

// TestMaterializeBytes_IgnoresNonCRDDocuments proves a multi-document rendered
// stream (the shape helm/kustomize output actually takes — many resources
// separated by "---", most of them not CRDs) only yields a schema for the
// CustomResourceDefinition document, mirroring Materialize's raw-tree behavior.
func TestMaterializeBytes_IgnoresNonCRDDocuments(t *testing.T) {
	t.Parallel()

	rendered := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
---
` + widgetCRD + `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg
`

	dest := t.TempDir()

	result, err := crdschema.MaterializeBytes(
		[]byte(rendered),
		"kustomization/app (rendered)",
		dest,
	)
	require.NoError(t, err)
	require.Equal(t, 1, result.Written)
	require.FileExists(t, filepath.Join(dest, "example.com", "widget_v1.json"))
}

// TestMaterializeBytes_WarningCarriesSource proves a warning from a malformed
// rendered document is attributed to the caller-supplied source label (e.g. the
// kustomization directory the stream was rendered from), not a file path — there
// is no file, since the content came from rendering rather than disk.
func TestMaterializeBytes_WarningCarriesSource(t *testing.T) {
	t.Parallel()

	const malformedCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: evil.example.com
spec:
  names:
    kind: Evil
  versions:
    - name: v1
      schema:
        openAPIV3Schema:
          type: object
`

	dest := t.TempDir()

	result, err := crdschema.MaterializeBytes([]byte(malformedCRD), "k8s/apps (rendered)", dest)
	require.NoError(t, err)
	require.Equal(t, 0, result.Written)
	require.Len(t, result.Warnings, 1)
	require.Equal(t, "k8s/apps (rendered)", result.Warnings[0].Source)
	require.Contains(t, result.Warnings[0].Reason, "spec.group")
}

// TestMaterializeBytes_DerivedSchemaValidatesViaKubeconform is the rendered-output
// counterpart of TestMaterialize_DerivedSchemaValidatesViaKubeconform: a schema
// derived from a CRD that only appears in a rendered manifest stream — never
// written to disk anywhere — still lets kubeconform catch an invalid custom
// resource of that kind, and pass a valid one.
func TestMaterializeBytes_DerivedSchemaValidatesViaKubeconform(t *testing.T) {
	t.Parallel()

	dest := t.TempDir()

	result, err := crdschema.MaterializeBytes(
		[]byte(widgetCRD),
		"helmrelease/widgets (rendered)",
		dest,
	)
	require.NoError(t, err)
	require.Equal(t, 1, result.Written)

	client := kubeconform.NewClient()
	ctx := context.Background()
	location := crdschema.SchemaLocation(dest)

	err = client.ValidateBytes(
		ctx,
		"widget.yaml",
		[]byte(validWidgetCR),
		&kubeconform.ValidationOptions{
			IgnoreMissingSchemas: false,
			SchemaLocations:      []string{location},
		},
	)
	require.NoError(t, err, "valid CR should pass against the rendered-source derived schema")

	err = client.ValidateBytes(
		ctx,
		"widget.yaml",
		[]byte(invalidWidgetCR),
		&kubeconform.ValidationOptions{
			IgnoreMissingSchemas: false,
			SchemaLocations:      []string{location},
		},
	)
	require.Error(t, err, "invalid CR should fail against the rendered-source derived schema")
}
