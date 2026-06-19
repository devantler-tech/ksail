package workload

import (
	"context"
	"fmt"
	"sync"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/client/kustomize"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/fluxsubst"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/render"
	"github.com/spf13/cobra"
)

// gitopsRenderer expands a kustomization directory into the manifests Flux
// actually applies: Kustomize build, Flux variable substitution, then in-process
// Helm rendering of HelmReleases. It is shared by the validate and scan commands.
type gitopsRenderer struct {
	kustomize *kustomize.Client
	opts      render.Options
}

// newGitOpsRenderer constructs a renderer backed by the in-process, kubeconfig-
// free Helm template client. The same client is reused across kustomizations.
func newGitOpsRenderer() (*gitopsRenderer, error) {
	helmClient, err := helm.NewTemplateOnlyClient()
	if err != nil {
		return nil, fmt.Errorf("create helm template client: %w", err)
	}

	return &gitopsRenderer{
		kustomize: kustomize.NewClient(),
		opts:      render.Options{Resolver: render.NewHelmChartResolver(helmClient)},
	}, nil
}

// expand builds, substitutes, and Helm-renders one kustomization directory. The
// kustomize build error is returned unwrapped so the caller's simplifyBuildError
// can strip the verbose "kustomize build <path>:" prefix.
func (g *gitopsRenderer) expand(ctx context.Context, kustDir string) (render.Result, error) {
	output, err := g.kustomize.Build(ctx, kustDir)
	if err != nil {
		return render.Result{}, err //nolint:wrapcheck // caller strips the kustomize prefix
	}

	expanded := fluxsubst.ExpandFluxSubstitutions(output.Bytes())

	result, err := render.Expand(ctx, expanded, g.opts)
	if err != nil {
		return render.Result{}, fmt.Errorf("expand HelmReleases: %w", err)
	}

	return result, nil
}

// degradationSink collects non-silent render degradations across parallel
// validation tasks. Warnings are reported after the progress group completes,
// because emitting them mid-group would interleave with the ANSI progress
// display. Silent degradations (e.g. a source object owned by a different
// kustomization) are dropped to avoid noise on large repos.
type degradationSink struct {
	mu   sync.Mutex
	list []render.Degradation
}

// add records the non-silent degradations from one render result.
func (s *degradationSink) add(degradations []render.Degradation) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, degradation := range degradations {
		if !degradation.Silent {
			s.list = append(s.list, degradation)
		}
	}
}

// report emits a warning per collected degradation to stderr.
func (s *degradationSink) report(cmd *cobra.Command) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, degradation := range s.list {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "skipped Helm render for HelmRelease %s (validating the resource as-is): %s",
			Args:    []any{degradation.HelmRelease, degradation.Reason},
			Writer:  cmd.ErrOrStderr(),
		})
	}
}
