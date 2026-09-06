package ephemeral_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/ephemeral"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func writeFixture(t *testing.T, root, path, data string) {
	t.Helper()

	path = filepath.Join(root, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
}

const configMap = `apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
data:
  mode: base
`

func TestLoadUsesSelectedOverlay(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "base/kustomization.yaml", "resources:\n- config.yaml\n")
	writeFixture(t, root, "base/config.yaml", configMap)
	writeFixture(t, root, "overlay/kustomization.yaml", `resources:
- ../base
patches:
- target:
    kind: ConfigMap
    name: settings
  patch: |-
    - op: replace
      path: /data/mode
      value: overlay
`)
	plan, err := ephemeral.Load(t.Context(), filepath.Join(root, "overlay"))
	require.NoError(t, err)
	require.Len(t, plan.Configuration, 1)
	mode, found, err := unstructured.NestedString(plan.Configuration[0].Object, "data", "mode")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "overlay", mode)
	assert.Empty(t, plan.Resources)
}

func TestLoadRejectsAmbiguousAndInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, data, want string }{
		{"malformed", "apiVersion: [", "decode"},
		{"no identity", "apiVersion: v1\nkind: ConfigMap\n", "metadata.name"},
		{"duplicate", configMap + "---\n" + configMap, "duplicate"},
		{"unresolved variable", configMap + "  unresolved: ${VALUE}\n", "substitution"},
		{"list", "apiVersion: v1\nkind: List\nitems: []\n", "List"},
		{
			"nested Flux",
			"apiVersion: kustomize.toolkit.fluxcd.io/v1\nkind: Kustomization\n" +
				"metadata:\n  name: nested\nspec:\n  path: ./child\n",
			"Flux Kustomization",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFixture(t, root, "input.yaml", testCase.data)
			_, err := ephemeral.Load(t.Context(), filepath.Join(root, "input.yaml"))
			require.ErrorContains(t, err, testCase.want)
		})
	}
}

func TestLoadClassifiesPrerequisites(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "input.yaml", `apiVersion: example.com/v1
kind: Widget
metadata:
  name: sample
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
---
apiVersion: v1
kind: Namespace
metadata:
  name: example
---
`+configMap)
	plan, err := ephemeral.Load(t.Context(), filepath.Join(root, "input.yaml"))
	require.NoError(t, err)
	require.Len(t, plan.Namespaces, 1)
	require.Len(t, plan.CRDs, 1)
	require.Len(t, plan.Configuration, 1)
	require.Len(t, plan.Resources, 1)
	assert.Equal(t, "Widget", plan.Resources[0].GetKind())
}

func TestLoadRejectsDirectoryContainingKustomizeRoots(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "base/kustomization.yaml", "resources: []\n")
	_, err := ephemeral.Load(t.Context(), root)
	require.ErrorContains(t, err, "select a Kustomize root")
}

func TestLoadChartAndWorkloadUseSameOverlay(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "base/kustomization.yaml", "resources:\n- chart.yaml\n")
	writeFixture(t, root, "base/chart.yaml", `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: operator
  namespace: operators
spec:
  chart:
    spec:
      chart: operator
      version: 1.0.0
      sourceRef:
        kind: HelmRepository
        name: charts
  values:
    replicas: 1
---
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata:
  name: charts
  namespace: operators
spec:
  url: https://example.com/charts
`)
	writeFixture(t, root, "overlay/kustomization.yaml", `resources:
- ../base
patches:
- target:
    kind: HelmRelease
    name: operator
  patch: |-
    - op: replace
      path: /spec/values/replicas
      value: 2
`)
	plan, err := ephemeral.Load(t.Context(), filepath.Join(root, "overlay"))
	require.NoError(t, err)
	require.Len(t, plan.Charts, 1)
	assert.Contains(t, plan.Charts[0].ValuesYaml, "replicas: 2")
	assert.Empty(t, plan.Resources, "Helm descriptors are installed through Helm")
}

func TestLoadDoesNotFollowEscapingSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	writeFixture(t, outside, "outside.yaml", configMap)
	require.NoError(
		t,
		os.Symlink(filepath.Join(outside, "outside.yaml"), filepath.Join(root, "escape.yaml")),
	)
	_, err := ephemeral.Load(t.Context(), root)
	require.Error(t, err)
}

func TestLoadPreservesIntegerValues(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "input.yaml", `apiVersion: example.com/v1
kind: Widget
metadata:
  name: example
spec:
  sequence: 9007199254740993
`)
	plan, err := ephemeral.Load(t.Context(), filepath.Join(root, "input.yaml"))
	require.NoError(t, err)
	require.Len(t, plan.Resources, 1)
	sequence, found, err := unstructured.NestedInt64(plan.Resources[0].Object, "spec", "sequence")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, int64(9007199254740993), sequence)
}

func TestLoadAcceptsCustomKindEndingInList(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(
		t,
		root,
		"input.yaml",
		"apiVersion: example.com/v1\nkind: ShoppingList\nmetadata:\n  name: example\nitems:\n- milk\n",
	)
	plan, err := ephemeral.Load(t.Context(), filepath.Join(root, "input.yaml"))
	require.NoError(t, err)
	require.Len(t, plan.Resources, 1)
}

func TestLoadRejectsDefaultNamespaceAlias(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(
		t,
		root,
		"input.yaml",
		configMap+"---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n  namespace: default\n",
	)
	_, err := ephemeral.Load(t.Context(), filepath.Join(root, "input.yaml"))
	require.ErrorContains(t, err, "duplicate")
}

func TestLoadRejectsMalformedHelmRelease(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(
		t,
		root,
		"input.yaml",
		"apiVersion: helm.toolkit.fluxcd.io/v2\nkind: HelmRelease\nmetadata:\n  name: example\nspec:\n  chart: []\n",
	)
	_, err := ephemeral.Load(t.Context(), filepath.Join(root, "input.yaml"))
	require.ErrorContains(t, err, "HelmRelease")
}

func TestLoadRejectsHelmPostRenderers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "input.yaml", `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: example
spec:
  postRenderers:
  - kustomize:
      images:
      - name: example
        newTag: test
`)
	_, err := ephemeral.Load(t.Context(), filepath.Join(root, "input.yaml"))
	require.ErrorContains(t, err, "postRenderers")
}

func TestLoadRequiresConfigurationNamespace(t *testing.T) {
	t.Parallel()

	for _, declared := range []bool{false, true} {
		t.Run(strconv.FormatBool(declared), func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()

			data := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: values\n  namespace: operators\n"
			if declared {
				data += "---\napiVersion: v1\nkind: Namespace\nmetadata:\n  name: operators\n"
			}

			writeFixture(t, root, "input.yaml", data)

			_, err := ephemeral.Load(t.Context(), filepath.Join(root, "input.yaml"))
			if declared {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "declare Namespace operators")
			}
		})
	}
}
