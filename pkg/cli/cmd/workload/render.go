package workload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/client/kustomize"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/fluxsubst"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/render"
	"github.com/spf13/cobra"
)

// renderedManifestPerm is the permission for the rendered manifests file written
// to the scan temp directory.
const renderedManifestPerm = 0o600

// gitopsRenderer expands a kustomization directory into the manifests Flux
// actually applies: Kustomize build, Flux variable substitution, then in-process
// Helm rendering of HelmReleases. It is shared by the validate and scan commands.
type gitopsRenderer struct {
	kustomize  *kustomize.Client
	chartCache *render.ChartCache
}

// newGitOpsRenderer constructs a renderer. The kustomize client is stateless and
// safe to share across goroutines; the Helm template client is created per
// kustomization in expand (see below). The chart cache is created once here so a
// chart referenced by several kustomizations in this run is templated once; it
// is scoped to this renderer (one run) so a floating chart version can't go
// stale across runs.
func newGitOpsRenderer() *gitopsRenderer {
	return &gitopsRenderer{
		kustomize:  kustomize.NewClient(),
		chartCache: render.NewChartCache(),
	}
}

// expand builds, substitutes, and Helm-renders one kustomization directory. The
// kustomize build error is returned unwrapped so the caller's simplifyBuildError
// can strip the verbose "kustomize build <path>:" prefix.
//
// A fresh Helm template client is created per call: helm's action.Configuration
// is not safe for concurrent use, and validate renders kustomizations in
// parallel, so each render must be isolated. Construction needs no cluster
// access and is cheap.
func (g *gitopsRenderer) expand(ctx context.Context, kustDir string) (render.Result, error) {
	output, err := g.kustomize.Build(ctx, kustDir)
	if err != nil {
		return render.Result{}, err //nolint:wrapcheck // caller strips the kustomize prefix
	}

	helmClient, err := helm.NewTemplateOnlyClient()
	if err != nil {
		return render.Result{}, fmt.Errorf("create helm template client: %w", err)
	}

	expanded := fluxsubst.ExpandFluxSubstitutions(output.Bytes())

	result, err := render.Expand(ctx, expanded, render.Options{
		Resolver: render.NewHelmChartResolver(
			helmClient,
			render.WithChartCache(g.chartCache),
		),
	})
	if err != nil {
		return render.Result{}, fmt.Errorf("expand HelmReleases: %w", err)
	}

	return result, nil
}

// renderToTempDir renders one kustomization directory to a fresh temp directory
// and returns it together with a cleanup func, so a file-based scanner
// (kubescape) can read the actually-applied manifests. Non-silent render
// degradations are warned to the user. The caller must invoke cleanup (e.g. via
// defer) to remove the temp directory.
func (g *gitopsRenderer) renderToTempDir(
	ctx context.Context,
	cmd *cobra.Command,
	kustDir string,
) (string, func(), error) {
	result, err := g.expand(ctx, kustDir)
	if err != nil {
		return "", nil, fmt.Errorf("render %q: %w", kustDir, err)
	}

	tmpDir, err := os.MkdirTemp("", "ksail-scan-*")
	if err != nil {
		return "", nil, fmt.Errorf("create scan temp dir: %w", err)
	}

	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	manifestPath := filepath.Join(tmpDir, "manifests.yaml")

	err = os.WriteFile(manifestPath, result.Bytes(), renderedManifestPerm)
	if err != nil {
		cleanup()

		return "", nil, fmt.Errorf("write rendered manifests: %w", err)
	}

	warnDegradations(cmd, result.Degradations)

	return tmpDir, cleanup, nil
}

// degradationSink collects render degradations across parallel validation tasks
// so they can be reported once after the progress group completes (emitting
// mid-group would interleave with the ANSI progress display).
type degradationSink struct {
	mu   sync.Mutex
	list []render.Degradation
}

// add records degradations from one render result for later reporting.
func (s *degradationSink) add(degradations []render.Degradation) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.list = append(s.list, degradations...)
}

// report warns about all collected degradations.
func (s *degradationSink) report(cmd *cobra.Command) {
	s.mu.Lock()
	defer s.mu.Unlock()

	warnDegradations(cmd, s.list)
}

// warnDegradations emits a warning for each non-silent render degradation. Silent
// degradations (e.g. a source object owned by a different kustomization) are
// skipped to avoid noise on large repos.
func warnDegradations(cmd *cobra.Command, degradations []render.Degradation) {
	for _, degradation := range degradations {
		if degradation.Silent {
			continue
		}

		content := "skipped Helm render for HelmRelease %s (validating the resource as-is): %s"

		switch degradation.Kind {
		case render.DegradationPartialValues:
			content = "rendered HelmRelease %s with incomplete values — valuesFrom %s " +
				"could not be resolved offline (cluster-managed or in another kustomization); " +
				"validation/scan may differ from the cluster"
		case render.DegradationMissingValuesKey:
			// The referent IS in the repo, so pointing at cluster-managed sources
			// would misdirect; the actionable cause is the key itself (often a typo).
			content = "rendered HelmRelease %s with incomplete values — valuesFrom %s " +
				"is in the repo but has no such key (Flux fails reconciliation on a " +
				"missing valuesKey, even when the reference is optional); " +
				"validation/scan may differ from the cluster"
		case render.DegradationSkippedRender:
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: content,
			Args:    []any{degradation.HelmRelease, degradation.Reason},
			Writer:  cmd.ErrOrStderr(),
		})
	}
}
