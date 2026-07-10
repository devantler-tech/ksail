// Copyright (c) KSail contributors. All rights reserved.
// Licensed under the PolyForm Shield License 1.0.0. See LICENSE in the project root.

package docs_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/docs"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// loadAPIFieldDocs loads doc comments from the v1alpha1 API package sources.
func loadAPIFieldDocs(t *testing.T) docs.FieldDocs {
	t.Helper()

	fieldDocs, err := docs.LoadFieldDocs("../pkg/apis/cluster/v1alpha1")
	if err != nil {
		t.Fatalf("loading API field docs: %v", err)
	}

	return fieldDocs
}

// TestLoadFieldDocs verifies that the loader captures both field-level and
// type-level comments from the API package.
func TestLoadFieldDocs(t *testing.T) {
	t.Parallel()

	fieldDocs := loadAPIFieldDocs(t)

	if doc := fieldDocs["OptionsHetzner"]["Location"]; !strings.Contains(
		doc,
		"datacenter location",
	) {
		t.Errorf("OptionsHetzner.Location doc comment not extracted, got %q", doc)
	}

	if doc := fieldDocs["OptionsOmni"][""]; !strings.Contains(doc, "Sidero Omni") {
		t.Errorf("OptionsOmni type-level doc comment not extracted, got %q", doc)
	}
}

// TestRenderFieldTableOptionsHetzner verifies that provider fields and defaults
// render while runtime-only fields stay out of the generated reference.
func TestRenderFieldTableOptionsHetzner(t *testing.T) {
	t.Parallel()

	table := docs.RenderFieldTable(
		reflect.TypeFor[v1alpha1.OptionsHetzner](), "", loadAPIFieldDocs(t),
	)

	expectations := []string{
		"| `controlPlaneServerType` |",
		"`cx23`",
		"| `workerPublicIPv4` | boolean |",
		"| `fallbackLocations` | []string |",
	}
	for _, want := range expectations {
		if !strings.Contains(table, want) {
			t.Errorf("OptionsHetzner table missing %q", want)
		}
	}

	if strings.Contains(table, "NodeAutoscalerEnabled") {
		t.Error("runtime-only json:\"-\" fields must not be documented")
	}
}

// TestRenderTypeSectionsProviderSpec verifies that nested provider types become
// linked reference sections with their provider-specific fields.
func TestRenderTypeSectionsProviderSpec(t *testing.T) {
	t.Parallel()

	sections := docs.RenderTypeSections(
		"spec.provider", reflect.TypeFor[v1alpha1.ProviderSpec](), loadAPIFieldDocs(t),
	)

	headings := []string{
		"### spec.provider (ProviderSpec)",
		"### spec.provider.hetzner (OptionsHetzner)",
		"### spec.provider.omni (OptionsOmni)",
		"### spec.provider.aws (OptionsAWS)",
		"### spec.provider.kubernetes (OptionsKubernetes)",
	}
	for _, heading := range headings {
		if !strings.Contains(sections, heading) {
			t.Errorf("provider sections missing heading %q", heading)
		}
	}

	if !strings.Contains(sections, "| `machineClass` |") {
		t.Error("provider sections missing OptionsOmni machineClass row")
	}
}

// TestRenderTypeSectionsAutoscaler verifies that nested autoscaler types render
// through the node-pool taint level.
func TestRenderTypeSectionsAutoscaler(t *testing.T) {
	t.Parallel()

	sections := docs.RenderTypeSections(
		"spec.cluster.autoscaler",
		reflect.TypeFor[v1alpha1.AutoscalerConfig](),
		loadAPIFieldDocs(t),
	)

	headings := []string{
		"### spec.cluster.autoscaler (AutoscalerConfig)",
		"### spec.cluster.autoscaler.node (NodeAutoscalerConfig)",
		"### spec.cluster.autoscaler.node.pools[] (NodePool)",
		"### spec.cluster.autoscaler.node.pools[].taints[] (NodePoolTaint)",
	}
	for _, heading := range headings {
		if !strings.Contains(sections, heading) {
			t.Errorf("autoscaler sections missing heading %q", heading)
		}
	}
}

// TestRenderFieldTablePreservesCommaSeparatedDescriptions verifies that schema
// descriptions retain prose commas instead of treating them as tag options.
func TestRenderFieldTablePreservesCommaSeparatedDescriptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		typeOf reflect.Type
		want   []string
	}{
		{
			name:   "cluster distribution config",
			typeOf: reflect.TypeFor[v1alpha1.ClusterSpec](),
			want: []string{
				"kind.yaml, k3d.yaml, vcluster.yaml, eks.yaml, or the talos directory",
			},
		},
		{
			name:   "node autoscaler aliases and expander chain",
			typeOf: reflect.TypeFor[v1alpha1.NodeAutoscalerConfig](),
			want: []string{
				"true=Enabled, false=Disabled",
				"[LeastNodes, LeastWaste]",
				"--expander=least-nodes,least-waste",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			table := docs.RenderFieldTable(test.typeOf, "", loadAPIFieldDocs(t))
			for _, want := range test.want {
				if !strings.Contains(table, want) {
					t.Errorf("rendered table missing complete description %q", want)
				}
			}
		})
	}
}

// TestConfigReferenceProviderSections verifies that the generated configuration
// reference includes the expected provider and autoscaler sections.
func TestConfigReferenceProviderSections(t *testing.T) {
	t.Parallel()

	text := readOrSkip(t, "src/content/docs/configuration/declarative-configuration.mdx")
	if !strings.Contains(text, "### spec.provider (ProviderSpec)") {
		t.Skip("config reference not yet regenerated (run go generate ./docs/...)")
	}

	expectations := []string{
		"### spec.provider.hetzner (OptionsHetzner)",
		"### spec.provider.omni (OptionsOmni)",
		"### spec.cluster.autoscaler (AutoscalerConfig)",
		"| `serverLimit` |",
		"| `machineClass` |",
	}
	for _, want := range expectations {
		if !strings.Contains(text, want) {
			t.Errorf("config reference missing %q", want)
		}
	}
}
