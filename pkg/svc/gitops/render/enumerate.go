package render

import (
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	"sigs.k8s.io/yaml"
)

// EnumerateChartSpecs parses the (kustomize-built, Flux-substituted) manifest
// stream and returns an install-ready helm.ChartSpec for every HelmRelease
// whose chart source resolves from in-stream objects (OCIRepository,
// HelmRepository, ConfigMap/Secret values) — the same resolution Expand
// performs before templating, surfaced for callers that need to INSTALL the
// declared charts rather than render them (ksail#5919 Phase 3b-2: --ephemeral
// installs a workload's declared operators into the throwaway cluster before
// its manifests are exercised against it).
//
// Mirroring Expand's degradation contract: a HelmRelease whose source cannot
// be resolved offline is recorded as a Degradation rather than failing the
// run, and an unparseable HelmRelease document is skipped (CR-schema
// validation reports it elsewhere).
func EnumerateChartSpecs(stream []byte) ([]*helm.ChartSpec, []Degradation) {
	docs, metas, index := parseSourceIndexedDocs(stream)

	var (
		specs        []*helm.ChartSpec
		degradations []Degradation
	)

	for docIndex, doc := range docs {
		if !isHelmRelease(metas[docIndex]) {
			continue
		}

		var helmRelease helmv2.HelmRelease

		err := yaml.Unmarshal(doc, &helmRelease)
		if err != nil {
			continue
		}

		spec, err := buildChartSpec(&helmRelease, index)
		if err != nil {
			degradations = append(degradations, Degradation{
				HelmRelease: helmRelease.Namespace + "/" + helmRelease.Name,
				Reason:      err.Error(),
				Err:         err,
				Silent:      isSilentDegradation(err),
			})

			continue
		}

		specs = append(specs, spec)
	}

	return specs, degradations
}
