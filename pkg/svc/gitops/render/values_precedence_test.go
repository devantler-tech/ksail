package render_test

import (
	"testing"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// Merging a root reference after inline values must fail this regression: Flux
// gives inline values precedence over ordinary ConfigMap and Secret references.
func TestBuildChartSpecInlineOverridesRootReference(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"ConfigMap", "Secret"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			helmRelease := chartRefRelease()
			helmRelease.Spec.Values = &apiextensionsv1.JSON{
				Raw: []byte(`{"replicaCount":3}`),
			}
			helmRelease.Spec.ValuesFrom = []fluxmeta.ValuesReference{
				{Kind: kind, Name: "base-values"},
			}
			sources := ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"})

			references := map[string]map[string]string{
				"flux-system/base-values": {"values.yaml": "replicaCount: 2\n"},
			}
			if kind == "ConfigMap" {
				sources.ConfigMaps = references
			} else {
				sources.Secrets = references
			}

			spec := resolveSpec(t, helmRelease, sources)
			values := unmarshalValues(t, spec.ValuesYaml)
			require.IsType(t, float64(0), values["replicaCount"])
			assert.InDelta(t, 3, values["replicaCount"], 0)
		})
	}
}

func TestBuildChartSpecRootReferencePrecedence(t *testing.T) {
	t.Parallel()

	helmRelease := chartRefRelease()
	helmRelease.Spec.Values = &apiextensionsv1.JSON{
		Raw: []byte(
			`{"settings":{"enabled":false,"count":0,"name":"","inline":"keep","nullable":null,"items":[]}}`,
		),
	}
	helmRelease.Spec.ValuesFrom = []fluxmeta.ValuesReference{
		{Kind: "ConfigMap", Name: "first"},
		{Kind: "Secret", Name: "last"},
	}
	sources := ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"})
	sources.ConfigMaps = map[string]map[string]string{
		"flux-system/first": {
			"values.yaml": "settings:\n  enabled: true\n  count: 8\n  name: first\n" +
				"  fromFirst: keep\n  ordered: first\n  nullable: old\n  items: [old]\n",
		},
	}
	sources.Secrets = map[string]map[string]string{
		"flux-system/last": {
			"values.yaml": "settings:\n  enabled: true\n  count: 9\n  name: last\n" +
				"  fromLast: keep\n  ordered: last\n",
		},
	}

	spec := resolveSpec(t, helmRelease, sources)
	values := unmarshalValues(t, spec.ValuesYaml)
	assert.Equal(t, map[string]any{
		"enabled": false, "count": float64(0), "name": "", "inline": "keep",
		"fromFirst": "keep", "fromLast": "keep", "ordered": "last",
		"nullable": nil, "items": []any{},
	}, values["settings"])
}

func TestBuildChartSpecLiteralTargetOverridesInline(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"false", "0", "",
		`{"enabled":false,"count":0,"items":[1,2],"value":"a=b,c.d"}`,
		"line: a,b=c[0]\npath: C:\\config\\app\n",
	} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			helmRelease := chartRefRelease()
			helmRelease.Spec.Values = &apiextensionsv1.JSON{
				Raw: []byte(`{"app":{"config":"inline","sibling":"keep"}}`),
			}
			helmRelease.Spec.ValuesFrom = []fluxmeta.ValuesReference{
				{
					Kind: "Secret", Name: "config", ValuesKey: "content",
					TargetPath: "app.config", Literal: true,
				},
			}
			sources := ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"})
			sources.Secrets = map[string]map[string]string{
				"flux-system/config": {"content": value},
			}

			spec := resolveSpec(t, helmRelease, sources)
			values := unmarshalValues(t, spec.ValuesYaml)
			assert.Equal(t, map[string]any{
				"config": value, "sibling": "keep",
			}, values["app"])
		})
	}
}

// A target assignment keeps its position in the reference list. Applying all
// target assignments in a second pass would incorrectly win over a later root.
func TestBuildChartSpecTargetReferenceOrder(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		references []fluxmeta.ValuesReference
		want       any
	}{
		{
			name: "target overrides inline",
			references: []fluxmeta.ValuesReference{
				{Kind: "ConfigMap", Name: "root"},
				{Kind: "Secret", Name: "target", ValuesKey: "count", TargetPath: "replicaCount"},
			},
			want: "4",
		},
		{
			name: "later root overrides earlier target",
			references: []fluxmeta.ValuesReference{
				{Kind: "Secret", Name: "target", ValuesKey: "count", TargetPath: "replicaCount"},
				{Kind: "ConfigMap", Name: "root"},
			},
			want: float64(2),
		},
		{
			name: "missing optional target preserves inline precedence",
			references: []fluxmeta.ValuesReference{
				{Kind: "Secret", Name: "missing", TargetPath: "replicaCount", Optional: true},
				{Kind: "ConfigMap", Name: "root"},
			},
			want: float64(3),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			helmRelease := chartRefRelease()
			helmRelease.Spec.Values = &apiextensionsv1.JSON{
				Raw: []byte(`{"replicaCount":3}`),
			}
			helmRelease.Spec.ValuesFrom = test.references
			sources := ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"})
			sources.ConfigMaps = map[string]map[string]string{
				"flux-system/root": {"values.yaml": "replicaCount: 2\n"},
			}
			sources.Secrets = map[string]map[string]string{
				"flux-system/target": {"count": "4"},
			}

			spec := resolveSpec(t, helmRelease, sources)
			values := unmarshalValues(t, spec.ValuesYaml)
			require.Contains(t, values, "replicaCount")
			assert.Equal(t, test.want, values["replicaCount"])
		})
	}
}
