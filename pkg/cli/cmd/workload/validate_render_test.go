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
// #5344. The HelmRelease CR itself is valid; only the rendered output is not.
func TestValidateCatchesInvalidRenderedManifest(t *testing.T) {
	t.Parallel()

	dir := writeHelmReleaseKustomization(t, localChartURL(t, "invalidchart"))

	_, err := runValidate(t, dir, "--skip-kinds", "OCIRepository")
	require.Error(t, err, "rendered invalid chart output should fail validation")
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
