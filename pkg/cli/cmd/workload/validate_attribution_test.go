package workload_test

import (
	"testing"

	workload "github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/render"
	"github.com/stretchr/testify/assert"
)

// attributionCase is a single attributionFromDocuments table entry.
type attributionCase struct {
	name string
	docs []render.Document
	want map[string]string
}

// renderedDoc builds a rendered (OriginRendered) document attributed to the given
// HelmRelease source ("namespace/name"), the case attributionFromDocuments maps.
func renderedDoc(kind, namespace, name, source string) render.Document {
	return render.Document{
		Kind:      kind,
		Namespace: namespace,
		Name:      name,
		Provenance: render.Provenance{
			Origin:            render.OriginRendered,
			SourceHelmRelease: source,
		},
	}
}

// runAttributionCases asserts attributionFromDocuments over a table of cases.
func runAttributionCases(t *testing.T, tests []attributionCase) {
	t.Helper()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportAttributionFromDocuments(testCase.docs)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// TestAttributionFromDocumentsMapping covers how documents are keyed and tagged:
// rendered docs get a Kind/Namespace/Name (or Kind/Name) key with their HelmRelease
// source, stream-origin and source-less docs are skipped, and identity-less docs are
// dropped.
func TestAttributionFromDocumentsMapping(t *testing.T) {
	t.Parallel()

	runAttributionCases(t, []attributionCase{
		{
			name: "namespaced rendered doc is tagged with its HelmRelease",
			docs: []render.Document{
				renderedDoc("ConfigMap", "default", "app-bad", "flux-system/app"),
			},
			want: map[string]string{"ConfigMap/default/app-bad": "HelmRelease flux-system/app"},
		},
		{
			name: "cluster-scoped rendered doc keys on Kind/Name",
			docs: []render.Document{
				renderedDoc("ClusterRole", "", "viewer", "flux-system/rbac"),
			},
			want: map[string]string{"ClusterRole/viewer": "HelmRelease flux-system/rbac"},
		},
		{
			name: "stream-origin docs are not attributed",
			docs: []render.Document{
				{Kind: "OCIRepository", Namespace: "flux-system", Name: "chart"},
			},
			want: nil,
		},
		{
			name: "rendered doc without a HelmRelease source is skipped",
			docs: []render.Document{
				{
					Kind:       "ConfigMap",
					Namespace:  "default",
					Name:       "loose",
					Provenance: render.Provenance{Origin: render.OriginRendered},
				},
			},
			want: nil,
		},
		{
			name: "docs missing a kind or name are dropped",
			docs: []render.Document{
				renderedDoc("", "default", "noKind", "flux-system/app"),
				renderedDoc("ConfigMap", "default", "", "flux-system/app"),
			},
			want: nil,
		},
	})
}

// TestAttributionFromDocumentsAmbiguity covers duplicate-identity handling: the same
// identity from one source is kept once, but an identity produced by two different
// sources is dropped as ambiguous rather than mis-attributed (while distinct identities
// survive).
func TestAttributionFromDocumentsAmbiguity(t *testing.T) {
	t.Parallel()

	runAttributionCases(t, []attributionCase{
		{
			name: "same identity from one source is kept once",
			docs: []render.Document{
				renderedDoc("ConfigMap", "default", "dup", "flux-system/app"),
				renderedDoc("ConfigMap", "default", "dup", "flux-system/app"),
			},
			want: map[string]string{"ConfigMap/default/dup": "HelmRelease flux-system/app"},
		},
		{
			name: "same identity from two sources is dropped as ambiguous",
			docs: []render.Document{
				renderedDoc("ConfigMap", "default", "clash", "flux-system/app-a"),
				renderedDoc("ConfigMap", "default", "clash", "flux-system/app-b"),
			},
			want: nil,
		},
		{
			name: "ambiguous identity is dropped while distinct ones are kept",
			docs: []render.Document{
				renderedDoc("ConfigMap", "default", "clash", "flux-system/app-a"),
				renderedDoc("ConfigMap", "default", "clash", "flux-system/app-b"),
				renderedDoc("Deployment", "web", "site", "flux-system/site"),
			},
			want: map[string]string{"Deployment/web/site": "HelmRelease flux-system/site"},
		},
	})
}
