package gen_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

// execHelmRelease runs the helmrelease subcommand with the provided args,
// returning stdout output and any execution error.
func execHelmRelease(t *testing.T, args []string) (string, error) {
	t.Helper()

	rt := di.NewRuntime()
	cmd := gen.NewHelmReleaseCmd(rt)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return buf.String(), err
}

func TestGenHelmReleaseSimple(t *testing.T) {
	t.Parallel()

	output, err := execHelmRelease(t, []string{
		"podinfo",
		"--source=HelmRepository/podinfo",
		"--chart=podinfo",
		"--export",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenHelmReleaseWithAllFlags(t *testing.T) {
	t.Parallel()

	output, err := execHelmRelease(t, []string{
		"webapp",
		"--namespace=production",
		"--source=HelmRepository/charts.flux-system",
		"--chart=webapp",
		"--chart-version=^1.0.0",
		"--interval=5m",
		"--timeout=10m",
		"--target-namespace=apps",
		"--storage-namespace=flux-system",
		"--create-target-namespace=true",
		"--service-account=webapp-sa",
		"--crds=CreateReplace",
		"--release-name=webapp-prod",
		"--export",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenHelmReleaseWithChartRef(t *testing.T) {
	t.Parallel()

	output, err := execHelmRelease(t, []string{
		"webapp",
		"--chart-ref=OCIRepository/webapp.flux-system",
		"--export",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenHelmReleaseWithDependencies(t *testing.T) {
	t.Parallel()

	output, err := execHelmRelease(t, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--depends-on=database",
		"--depends-on=production/redis",
		"--export",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenHelmReleaseWithValuesFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	valuesFile := filepath.Join(tmpDir, "values.yaml")

	err := os.WriteFile(valuesFile, []byte("image:\n  tag: v2.0.0\nreplicaCount: 3\n"), 0o600)
	require.NoError(t, err)

	output, err := execHelmRelease(t, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--values=" + valuesFile,
		"--export",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenHelmReleaseWithValuesFrom(t *testing.T) {
	t.Parallel()

	output, err := execHelmRelease(t, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--values-from=Secret/my-values",
		"--values-from=ConfigMap/common-config",
		"--export",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenHelmReleaseWithVersion(t *testing.T) {
	t.Parallel()

	output, err := execHelmRelease(t, []string{
		"webapp",
		"--namespace=production",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--chart-version=^1.0.0",
		"--export",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenHelmReleaseMissingSourceAndRef(t *testing.T) {
	t.Parallel()

	_, err := execHelmRelease(t, []string{
		"webapp",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "either --source with --chart or --chart-ref must be specified")
}

func TestGenHelmReleaseMissingChart(t *testing.T) {
	t.Parallel()

	_, err := execHelmRelease(t, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "either --source with --chart or --chart-ref must be specified")
}

func TestGenHelmReleaseConflictingSourceAndChartRef(t *testing.T) {
	t.Parallel()

	_, err := execHelmRelease(t, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--chart-ref=OCIRepository/webapp",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot specify both --source/--chart and --chart-ref")
}

func TestGenHelmReleaseInvalidSourceKind(t *testing.T) {
	t.Parallel()

	_, err := execHelmRelease(t, []string{
		"webapp",
		"--source=InvalidKind/charts",
		"--chart=webapp",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid kind")
}

func TestGenHelmReleaseInvalidChartRefKind(t *testing.T) {
	t.Parallel()

	_, err := execHelmRelease(t, []string{
		"webapp",
		"--chart-ref=InvalidKind/webapp",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid kind")
}

func TestGenHelmReleaseInvalidCRDsPolicy(t *testing.T) {
	t.Parallel()

	_, err := execHelmRelease(t, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--crds=InvalidPolicy",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid kind")
}

func TestGenHelmReleaseWithoutExport(t *testing.T) {
	t.Parallel()

	_, err := execHelmRelease(t, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "not yet implemented")
}

func TestGenHelmReleaseInvalidSourceFormat(t *testing.T) {
	t.Parallel()

	_, err := execHelmRelease(t, []string{
		"webapp",
		"--source=HelmRepositoryMissingSlash",
		"--chart=webapp",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid format")
}

func TestGenHelmReleaseInvalidDependencyFormat(t *testing.T) {
	t.Parallel()

	_, err := execHelmRelease(t, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--depends-on=a/b/c",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid depends-on format")
}

func TestGenHelmReleaseNonExistentValuesFile(t *testing.T) {
	t.Parallel()

	_, err := execHelmRelease(t, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--values=/nonexistent/path/values.yaml",
		"--export",
	})

	require.Error(t, err)
}

func TestGenHelmReleaseWithGitRepositorySource(t *testing.T) {
	t.Parallel()

	output, err := execHelmRelease(t, []string{
		"webapp",
		"--source=GitRepository/my-repo",
		"--chart=./charts/webapp",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, output, "GitRepository")
}

func TestGenHelmReleaseWithBucketSource(t *testing.T) {
	t.Parallel()

	output, err := execHelmRelease(t, []string{
		"webapp",
		"--source=Bucket/my-bucket",
		"--chart=webapp",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, output, "Bucket")
}

func TestGenHelmReleaseWithHelmChartRef(t *testing.T) {
	t.Parallel()

	output, err := execHelmRelease(t, []string{
		"webapp",
		"--chart-ref=HelmChart/webapp.flux-system",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, output, "HelmChart")
}

func TestGenHelmReleaseWithKubeconfigSecretRef(t *testing.T) {
	t.Parallel()

	output, err := execHelmRelease(t, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--kubeconfig-secret-ref=my-kubeconfig",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, output, "my-kubeconfig")
}

func TestGenHelmReleaseWithCaseSensitiveValuesFrom(t *testing.T) {
	t.Parallel()

	// validateKindCaseInsensitive should normalize "secret" to "Secret"
	output, err := execHelmRelease(t, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--values-from=secret/my-values",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, output, "kind: Secret")
}

func TestGenHelmReleaseInvalidValuesFromKind(t *testing.T) {
	t.Parallel()

	_, err := execHelmRelease(t, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--values-from=Deployment/my-config",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid kind")
}

func TestGenHelmReleaseSrcNamespaceFromDot(t *testing.T) {
	t.Parallel()

	// HelmRepository/name.namespace - namespace extracted from dot notation
	output, err := execHelmRelease(t, []string{
		"webapp",
		"--source=HelmRepository/charts.custom-ns",
		"--chart=webapp",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, output, "custom-ns")
}

func TestGenHelmReleaseRequiresName(t *testing.T) {
	t.Parallel()

	_, err := execHelmRelease(t, []string{
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--export",
	})

	require.Error(t, err)
}
