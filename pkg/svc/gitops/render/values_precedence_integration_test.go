package render_test

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/render"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	chartloader "helm.sh/helm/v4/pkg/chart/v2/loader"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	"sigs.k8s.io/yaml"
)

// This boundary test uses an actual packaged chart and Helm template execution.
// Reversing inline/reference precedence must change the rendered child to 2.
func TestExpandInlinePrecedenceWithRealHelmChart(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("HELM_REPOSITORY_CACHE", cache)
	t.Setenv("HELM_REPOSITORY_CONFIG", filepath.Join(cache, "repositories.yaml"))
	t.Setenv("HELM_REGISTRY_CONFIG", filepath.Join(cache, "registry.json"))

	server := precedenceChartRepository(t)
	client, err := helm.NewTemplateOnlyClient()
	require.NoError(t, err)

	resolver := render.NewHelmChartResolver(client)

	// The parent changes Helm's process environment; subtests must run serially.
	for _, kind := range []string{"ConfigMap", "Secret"} { //nolint:paralleltest
		t.Run(kind, func(t *testing.T) {
			result, expandErr := render.Expand(
				t.Context(),
				[]byte(precedenceChartStream(server.URL, kind)),
				render.Options{Resolver: resolver},
			)
			require.NoError(t, expandErr)
			require.Empty(t, result.Degradations)

			found := false

			for _, document := range result.Documents {
				if document.Provenance.Origin != render.OriginRendered {
					continue
				}

				var child struct {
					Data map[string]string `json:"data"`
				}
				require.NoError(t, yaml.Unmarshal(document.Bytes, &child))
				assert.Equal(t, "3", child.Data["replicaCount"])
				assert.Equal(t, "flux-system/probe", document.Provenance.SourceHelmRelease)

				found = true
			}

			require.True(t, found, "the fixture must produce a real Helm-rendered child")
		})
	}
}

func precedenceChartRepository(t *testing.T) *httptest.Server {
	t.Helper()

	chart, err := chartloader.Load("testdata/values-precedence")
	require.NoError(t, err)
	repository := t.TempDir()
	_, err = chartutil.Save(chart, repository)
	require.NoError(t, err)

	index := `apiVersion: v1
entries:
  values-probe:
    - apiVersion: v2
      name: values-probe
      version: 0.1.0
      urls: [values-probe-0.1.0.tgz]
`
	require.NoError(t, os.WriteFile(filepath.Join(repository, "index.yaml"), []byte(index), 0o600))
	server := httptest.NewServer(http.FileServer(http.Dir(repository)))
	t.Cleanup(server.Close)

	return server
}

func precedenceChartStream(repositoryURL, kind string) string {
	referencedValues := "replicaCount: 2\n"
	if kind == "Secret" {
		referencedValues = base64.StdEncoding.EncodeToString([]byte(referencedValues))
	}

	return fmt.Sprintf(`apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata:
  name: fixture
  namespace: flux-system
spec:
  url: %s
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: probe
  namespace: flux-system
spec:
  chart:
    spec:
      chart: values-probe
      version: 0.1.0
      sourceRef:
        kind: HelmRepository
        name: fixture
  valuesFrom:
    - kind: %s
      name: base-values
  values:
    replicaCount: 3
---
apiVersion: v1
kind: %s
metadata:
  name: base-values
  namespace: flux-system
data:
  values.yaml: %q
`, repositoryURL, kind, kind, referencedValues)
}
