package render_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/render"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// TestExpandIndexesAllSourceKinds drives indexSource/extractStringData over a
// stream containing a HelmRepository, a ConfigMap and a Secret, and asserts the
// resolver receives them in the SourceIndex.
func TestExpandIndexesAllSourceKinds(t *testing.T) {
	t.Parallel()

	helmRepoYAML := `apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata:
  name: repo
  namespace: flux-system
spec:
  url: https://example.com/charts`

	cmYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: flux-system
data:
  values.yaml: "key: cmval"`

	secretYAML := `apiVersion: v1
kind: Secret
metadata:
  name: sec
  namespace: flux-system
data:
  values.yaml: ` + base64.StdEncoding.EncodeToString([]byte("key: secval")) + `
stringData:
  plain: hello`

	hrYAML := `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: app
  namespace: flux-system
spec:
  chart:
    spec:
      chart: app
      sourceRef:
        kind: HelmRepository
        name: repo`

	resolver := fakeResolver{
		render: func(_ *helmv2.HelmRelease, sources render.SourceIndex) (string, error) {
			assert.Contains(t, sources.HelmRepos, "flux-system/repo")
			// extractStringData stores the data map verbatim; the nested YAML is
			// parsed later by applyValuesFrom, not here.
			assert.Equal(t, "key: cmval", sources.ConfigMaps["flux-system/cm"]["values.yaml"])
			assert.Equal(t, "key: secval", sources.Secrets["flux-system/sec"]["values.yaml"])
			assert.Equal(t, "hello", sources.Secrets["flux-system/sec"]["plain"])

			return "apiVersion: v1\nkind: Service\nmetadata:\n  name: app", nil
		},
	}

	result, err := render.Expand(
		context.Background(),
		joinDocs(helmRepoYAML, cmYAML, secretYAML, hrYAML),
		render.Options{Resolver: resolver},
	)
	require.NoError(t, err)
	assert.Empty(t, result.Degradations)
}

// TestSortDocumentsOrdersByNamespaceAndName exercises the namespace, name and
// byte tiebreakers in sortDocuments via rendered children.
func TestSortDocumentsOrdersByNamespaceAndName(t *testing.T) {
	t.Parallel()

	children := strings.Join([]string{
		"apiVersion: v1\nkind: Service\nmetadata:\n  name: zeta\n  namespace: alpha",
		"apiVersion: v1\nkind: Service\nmetadata:\n  name: beta\n  namespace: alpha",
		"apiVersion: v1\nkind: Service\nmetadata:\n  name: alpha\n  namespace: omega",
	}, "\n---\n")

	resolver := fakeResolver{
		render: func(_ *helmv2.HelmRelease, _ render.SourceIndex) (string, error) {
			return children, nil
		},
	}

	result, err := render.Expand(
		context.Background(),
		joinDocs(helmReleaseYAML),
		render.Options{Resolver: resolver},
	)
	require.NoError(t, err)

	out := string(result.Bytes())
	// namespace alpha sorts before omega; within alpha, name beta before zeta.
	betaPos := strings.Index(out, "name: beta")
	zetaPos := strings.Index(out, "name: zeta")
	alphaPos := strings.Index(out, "name: alpha")

	assert.Less(t, betaPos, zetaPos)
	assert.Less(t, zetaPos, alphaPos)
}

// TestBuildChartSpecValuesFromSecret covers the Secret branch of applyValuesFrom.
func TestBuildChartSpecValuesFromSecret(t *testing.T) {
	t.Parallel()

	helmRelease := chartRefRelease()
	helmRelease.Spec.ValuesFrom = []fluxmeta.ValuesReference{
		{Kind: "Secret", Name: "creds", ValuesKey: "secret-values.yaml"},
	}

	sources := ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"})
	sources.Secrets = map[string]map[string]string{
		//nolint:gosec // test fixture value, not a real credential
		"flux-system/creds": {"secret-values.yaml": "auth:\n  token: abc"},
	}

	spec := resolveSpec(t, helmRelease, sources)

	assert.Contains(t, spec.ValuesYaml, "token: abc")
}

// TestBuildChartSpecDeepMergesValues covers nested merging in mergeValues.
func TestBuildChartSpecDeepMergesValues(t *testing.T) {
	t.Parallel()

	helmRelease := chartRefRelease()
	helmRelease.Spec.Values = &apiextensionsv1.JSON{Raw: []byte(`{"service":{"port":80}}`)}
	helmRelease.Spec.ValuesFrom = []fluxmeta.ValuesReference{{Kind: "ConfigMap", Name: "extra"}}

	sources := ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"})
	sources.ConfigMaps = map[string]map[string]string{
		"flux-system/extra": {"values.yaml": "service:\n  type: LoadBalancer"},
	}

	spec := resolveSpec(t, helmRelease, sources)

	assert.Contains(t, spec.ValuesYaml, "port: 80")
	assert.Contains(t, spec.ValuesYaml, "type: LoadBalancer")
}

// TestBuildChartSpecValuesFromTargetPathMergesWithInline verifies a targetPath
// reference is injected at its path without clobbering inline spec.values.
func TestBuildChartSpecValuesFromTargetPathMergesWithInline(t *testing.T) {
	t.Parallel()

	helmRelease := chartRefRelease()
	helmRelease.Spec.Values = &apiextensionsv1.JSON{Raw: []byte(`{"keep":true}`)}
	helmRelease.Spec.ValuesFrom = []fluxmeta.ValuesReference{
		{Kind: "ConfigMap", Name: "extra", ValuesKey: "env", TargetPath: "some.path"},
	}

	sources := ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"})
	sources.ConfigMaps = map[string]map[string]string{
		"flux-system/extra": {"env": "prod"},
	}

	spec := resolveSpec(t, helmRelease, sources)

	values := unmarshalValues(t, spec.ValuesYaml)
	assert.Equal(t, true, values["keep"])
	some, _ := values["some"].(map[string]any)
	assert.Equal(t, "prod", some["path"])
}

// TestApplyOCIRepositoryNoReference covers an OCIRepository without a ref.
func TestApplyOCIRepositoryNoReference(t *testing.T) {
	t.Parallel()

	spec := resolveSpec(t, chartRefRelease(), ociIndex(nil))

	assert.Equal(t, "oci://ghcr.io/stefanprodan/charts/podinfo", spec.ChartName)
	assert.Empty(t, spec.Version)
}
