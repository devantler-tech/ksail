package workload_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v7/pkg/client/kustomize"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errRenderBuild = errors.New("fixture build failure")

func TestCRDDiscoveryReusesEachKustomizationForValidation(t *testing.T) {
	t.Parallel()

	for _, widget := range []struct {
		name     string
		manifest string
		invalid  bool
	}{
		{name: "valid", manifest: validWidget},
		{name: "invalid", manifest: invalidWidget, invalid: true},
	} {
		t.Run(widget.name, func(t *testing.T) {
			t.Parallel()

			dir := writeRenderedCRDKustomization(t, widget.manifest)
			schemas := t.TempDir()

			var builds atomic.Int32

			renderer := workload.ExportNewValidationRenderer(
				func(ctx context.Context, path string) (*bytes.Buffer, error) {
					builds.Add(1)

					return kustomize.NewClient().Build(ctx, path)
				},
			)
			result, err := renderer.Discover(t.Context(), dir, schemas)
			require.NoError(t, err)
			require.Equal(t, 1, result.Written)

			var output bytes.Buffer

			cmd := &cobra.Command{}
			cmd.SetOut(&output)
			cmd.SetErr(&output)

			err = renderer.Validate(t.Context(), cmd, dir, schemas, false)
			if widget.invalid {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "bogus")
			} else {
				require.NoError(t, err)
			}

			assert.Equal(
				t,
				int32(1),
				builds.Load(),
				"CRD discovery and validation must share one render",
			)
		})
	}
}

func TestCRDDiscoveryRetainsRenderFailure(t *testing.T) {
	t.Parallel()

	dir := writeRenderedCRDKustomization(t, validWidget)

	var builds atomic.Int32

	renderer := workload.ExportNewValidationRenderer(
		func(context.Context, string) (*bytes.Buffer, error) {
			if builds.Add(1) > 1 {
				return bytes.NewBufferString(validWidget), nil
			}

			return nil, errRenderBuild
		},
	)
	result, err := renderer.Discover(t.Context(), dir, t.TempDir())
	require.NoError(t, err, "the discovery pass retains its warning behavior")
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0].Reason, errRenderBuild.Error())
	_, err = renderer.Expand(t.Context(), dir)
	require.ErrorIs(t, err, errRenderBuild, "the validation pass must still fail")
	assert.Equal(t, int32(1), builds.Load())
}

func TestCRDDiscoveryReusesParallelTargets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	const targets = 8
	for target := range targets {
		dir := filepath.Join(root, fmt.Sprintf("app-%d", target))
		require.NoError(t, os.Mkdir(dir, 0o700))
		writeKustomizationFiles(t, dir, localChartURL(t, "validchart"))
	}

	var builds atomic.Int32

	renderer := workload.ExportNewValidationRenderer(
		func(ctx context.Context, path string) (*bytes.Buffer, error) {
			builds.Add(1)

			return kustomize.NewClient().Build(ctx, path)
		},
	)
	_, err := renderer.Discover(t.Context(), root, t.TempDir())
	require.NoError(t, err)

	var workers sync.WaitGroup
	for target := range targets {
		workers.Go(func() {
			result, err := renderer.Expand(
				t.Context(),
				filepath.Join(root, fmt.Sprintf("app-%d", target)),
			)
			if err != nil {
				t.Errorf("render target %d: %v", target, err)

				return
			}

			assert.NotEmpty(t, result.Documents)
		})
	}

	workers.Wait()
	assert.Equal(t, int32(targets), builds.Load())
}

func TestCRDDiscoveryConsumesSnapshotOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(dir, "kustomization.yaml"),
			[]byte("resources: [resource.yaml]\n"),
			0o600,
		),
	)
	file := filepath.Join(dir, "resource.yaml")
	first := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: first\n"
	second := strings.ReplaceAll(first, "first", "second")
	require.NoError(t, os.WriteFile(file, []byte(first), 0o600))

	var builds atomic.Int32

	build := func(ctx context.Context, path string) (*bytes.Buffer, error) {
		builds.Add(1)

		return kustomize.NewClient().Build(ctx, path)
	}
	renderer := workload.ExportNewValidationRenderer(build)
	_, err := renderer.Discover(t.Context(), dir, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, []byte(second), 0o600))

	result, err := renderer.Expand(t.Context(), dir)
	require.NoError(t, err)
	assert.Contains(
		t,
		string(result.Bytes()),
		"name: first",
		"validation must inspect the discovery snapshot",
	)
	assert.Equal(t, int32(1), builds.Load())

	result, err = renderer.Expand(t.Context(), dir)
	require.NoError(t, err)
	assert.Contains(
		t,
		string(result.Bytes()),
		"name: second",
		"consumed manifests must not become a persistent cache",
	)
	assert.Equal(t, int32(2), builds.Load())

	nextRun := workload.ExportNewValidationRenderer(build)
	result, err = nextRun.Expand(t.Context(), dir)
	require.NoError(t, err)
	assert.Contains(t, string(result.Bytes()), "name: second")
	assert.Equal(t, int32(3), builds.Load(), "each new invocation renders independently")
}

func TestCRDDiscoveryCancellationDoesNotReturnPreparedSuccess(t *testing.T) {
	t.Parallel()

	dir := writeRenderedCRDKustomization(t, validWidget)

	var builds atomic.Int32

	renderer := workload.ExportNewValidationRenderer(
		func(ctx context.Context, path string) (*bytes.Buffer, error) {
			builds.Add(1)

			return kustomize.NewClient().Build(ctx, path)
		},
	)
	_, err := renderer.Discover(t.Context(), dir, t.TempDir())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = renderer.Expand(ctx, dir)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int32(1), builds.Load())
}

func TestCRDDiscoveryPreservesRenderMetadata(t *testing.T) {
	t.Parallel()

	for _, chart := range []string{"validchart", "invalidchart", "missing-chart"} {
		t.Run(chart, func(t *testing.T) {
			t.Parallel()

			dir := writeHelmReleaseKustomization(t, localChartURL(t, chart))
			baseline := workload.ExportNewValidationRenderer(nil)
			want, err := baseline.Expand(t.Context(), dir)
			require.NoError(t, err)

			renderer := workload.ExportNewValidationRenderer(nil)
			_, err = renderer.Discover(t.Context(), dir, t.TempDir())
			require.NoError(t, err)
			got, err := renderer.Expand(t.Context(), dir)
			require.NoError(t, err)
			assert.Equal(
				t,
				want,
				got,
				"provenance and degradations must survive along with the bytes",
			)

			if chart == "missing-chart" {
				require.NotEmpty(t, got.Degradations)
			} else {
				require.NotEmpty(t, got.Documents)
			}
		})
	}
}

func TestValidateCRDSchemasPreservesHelmAttribution(t *testing.T) {
	t.Parallel()

	dir := writeHelmReleaseKustomization(t, localChartURL(t, "invalidchart"))
	_, err := runValidate(t, dir, "--include-crd-schemas", "--skip-kinds", "OCIRepository")
	require.ErrorContains(t, err, "(from HelmRelease flux-system/app)")
}

func TestValidateCRDSchemasReportsDegradationOnce(t *testing.T) {
	t.Parallel()

	dir := writeHelmReleaseKustomization(t, localChartURL(t, "missing-chart"))
	output, err := runValidate(
		t,
		dir,
		"--include-crd-schemas",
		"--skip-kinds",
		"OCIRepository,HelmRelease",
	)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(output, "skipped Helm render"))
}

func writeRenderBenchmarkTargets(b *testing.B, targets, resources int) string {
	b.Helper()

	root := b.TempDir()

	for target := range targets {
		dir := filepath.Join(root, fmt.Sprintf("app-%d", target))
		require.NoError(b, os.Mkdir(dir, 0o700))

		var manifests bytes.Buffer
		for resource := range resources {
			fmt.Fprintf(
				&manifests,
				"---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: config-%d\ndata:\n  value: example\n",
				resource,
			)
		}

		require.NoError(
			b,
			os.WriteFile(filepath.Join(dir, "resources.yaml"), manifests.Bytes(), 0o600),
		)
		require.NoError(
			b,
			os.WriteFile(
				filepath.Join(dir, "kustomization.yaml"),
				[]byte("resources: [resources.yaml]\n"),
				0o600,
			),
		)
	}

	return root
}

// BenchmarkCRDDiscoveryAndValidationRender measures actual Kustomize/Flux work in both passes.
// The renderer is new per iteration, matching a command invocation; no cross-run cache is warmed.
func BenchmarkCRDDiscoveryAndValidationRender(b *testing.B) {
	const (
		targets   = 8
		resources = 32
	)

	root := writeRenderBenchmarkTargets(b, targets, resources)
	schemas := b.TempDir()

	var builds int

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		renderer := workload.ExportNewValidationRenderer(
			func(ctx context.Context, path string) (*bytes.Buffer, error) {
				builds++

				return kustomize.NewClient().Build(ctx, path)
			},
		)
		_, err := renderer.Discover(b.Context(), root, schemas)
		require.NoError(b, err)

		for target := range targets {
			_, err := renderer.Expand(
				b.Context(),
				filepath.Join(root, fmt.Sprintf("app-%d", target)),
			)
			require.NoError(b, err)
		}
	}

	b.ReportMetric(float64(builds)/float64(b.N), "builds/op")
}
